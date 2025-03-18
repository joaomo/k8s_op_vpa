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
	list := &appsv1.DaemonSetList{}

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
		workloads[i] = &DaemonSetWorkload{&list.Items[i]}
	}
	return workloads, nil
}

func (p *DaemonSetProvider) NewObject() client.Object {
	return &appsv1.DaemonSet{}
}
