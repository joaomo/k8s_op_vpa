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
	list := &appsv1.StatefulSetList{}

	listOpts := []client.ListOption{client.InNamespace(namespace)}

	if selector != nil {
		labelSelector, err := metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			return nil, err
		}
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: labelSelector})
	}

	if err := c.List(ctx, list, listOpts...); err != nil {
		return nil, err
	}

	workloads := make([]Workload, len(list.Items))
	for i := range list.Items {
		workloads[i] = &StatefulSetWorkload{&list.Items[i]}
	}
	return workloads, nil
}

func (p *StatefulSetProvider) NewObject() client.Object {
	return &appsv1.StatefulSet{}
}
