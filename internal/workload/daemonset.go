package workload

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DaemonSetWorkload wraps a DaemonSet to implement the Workload interface
type DaemonSetWorkload struct {
	*appsv1.DaemonSet
}

func (d *DaemonSetWorkload) GetKind() string       { return "DaemonSet" }
func (d *DaemonSetWorkload) GetAPIVersion() string { return "apps/v1" }
func (d *DaemonSetWorkload) GetUID() types.UID     { return d.UID }

// DaemonSetProvider provides DaemonSet workloads
type DaemonSetProvider struct{}

func (p *DaemonSetProvider) Kind() string { return "DaemonSet" }

func (p *DaemonSetProvider) List(ctx context.Context, c client.Client, namespace string, selector *metav1.LabelSelector) ([]Workload, error) {
	var workloads []Workload
	err := p.ForEach(ctx, c, namespace, selector, func(w Workload) (bool, error) {
		workloads = append(workloads, w)
		return true, nil
	})
	return workloads, err
}

func (p *DaemonSetProvider) ForEach(ctx context.Context, c client.Client, namespace string, selector *metav1.LabelSelector, callback WorkloadCallback) error {
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
		list := &appsv1.DaemonSetList{}
		opts := listOpts
		if continueToken != "" {
			opts = append(opts, client.Continue(continueToken))
		}

		if err := c.List(ctx, list, opts...); err != nil {
			return err
		}

		for i := range list.Items {
			continueIteration, err := callback(&DaemonSetWorkload{&list.Items[i]})
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

func (p *DaemonSetProvider) NewObject() client.Object {
	return &appsv1.DaemonSet{}
}
