package workload

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StatefulSetWorkload wraps a StatefulSet to implement the Workload interface
type StatefulSetWorkload struct {
	*appsv1.StatefulSet
}

func (s *StatefulSetWorkload) GetKind() string       { return "StatefulSet" }
func (s *StatefulSetWorkload) GetAPIVersion() string { return "apps/v1" }
func (s *StatefulSetWorkload) GetUID() types.UID     { return s.UID }

// StatefulSetProvider provides StatefulSet workloads
type StatefulSetProvider struct{}

func (p *StatefulSetProvider) Kind() string { return "StatefulSet" }

func (p *StatefulSetProvider) List(ctx context.Context, c client.Client, namespace string, selector *metav1.LabelSelector) ([]Workload, error) {
	var workloads []Workload
	err := p.ForEach(ctx, c, namespace, selector, func(w Workload) (bool, error) {
		workloads = append(workloads, w)
		return true, nil
	})
	return workloads, err
}

func (p *StatefulSetProvider) ForEach(ctx context.Context, c client.Client, namespace string, selector *metav1.LabelSelector, callback WorkloadCallback) error {
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.Limit(PageSize),
	}

	if selector != nil {
		labelSelector, err := metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			return err
		}
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: labelSelector})
	}

	var continueToken string
	for {
		list := &appsv1.StatefulSetList{}
		opts := listOpts
		if continueToken != "" {
			opts = append(opts, client.Continue(continueToken))
		}

		if err := c.List(ctx, list, opts...); err != nil {
			return err
		}

		for i := range list.Items {
			continueIteration, err := callback(&StatefulSetWorkload{&list.Items[i]})
			if err != nil {
				return err
			}
			if !continueIteration {
				return nil
			}
		}

		continueToken = list.GetContinue()
		if continueToken == "" {
			break
		}
	}
	return nil
}

func (p *StatefulSetProvider) NewObject() client.Object {
	return &appsv1.StatefulSet{}
}
