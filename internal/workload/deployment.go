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
	var workloads []Workload
	err := p.ForEach(ctx, c, namespace, selector, func(w Workload) (bool, error) {
		workloads = append(workloads, w)
		return true, nil
	})
	return workloads, err
}

func (p *DeploymentProvider) ForEach(ctx context.Context, c client.Client, namespace string, selector *metav1.LabelSelector, callback WorkloadCallback) error {
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
		list := &appsv1.DeploymentList{}
		opts := listOpts
		if continueToken != "" {
			opts = append(opts, client.Continue(continueToken))
		}

		if err := c.List(ctx, list, opts...); err != nil {
			return err
		}

		for i := range list.Items {
			continueIteration, err := callback(&DeploymentWorkload{&list.Items[i]})
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

func (p *DeploymentProvider) NewObject() client.Object {
	return &appsv1.Deployment{}
}
