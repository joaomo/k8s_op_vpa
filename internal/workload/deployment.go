package workload

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeploymentWorkload wraps a Deployment to implement the Workload interface
type DeploymentWorkload struct {
	*appsv1.Deployment
}

func (d *DeploymentWorkload) GetKind() string       { return "Deployment" }
func (d *DeploymentWorkload) GetAPIVersion() string { return "apps/v1" }
func (d *DeploymentWorkload) GetUID() types.UID     { return d.UID }

// DeploymentProvider provides Deployment workloads
type DeploymentProvider struct{}

func (p *DeploymentProvider) Kind() string { return "Deployment" }

func (p *DeploymentProvider) List(ctx context.Context, c client.Client, namespace string, selector *metav1.LabelSelector) ([]Workload, error) {
	list := &appsv1.DeploymentList{}

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
		workloads[i] = &DeploymentWorkload{&list.Items[i]}
	}
	return workloads, nil
}

func (p *DeploymentProvider) NewObject() client.Object {
	return &appsv1.Deployment{}
}
