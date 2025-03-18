package workload

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Workload abstracts Deployment, StatefulSet, DaemonSet for VPA management
type Workload interface {
	GetName() string
	GetNamespace() string
	GetUID() types.UID
	GetLabels() map[string]string
	GetKind() string
	GetAPIVersion() string
}

// Provider lists and matches workloads of a specific type
type Provider interface {
	// Kind returns the workload kind (e.g., "Deployment", "StatefulSet", "DaemonSet")
	Kind() string

	// List returns all workloads in a namespace matching the selector
	List(ctx context.Context, c client.Client, namespace string, selector *metav1.LabelSelector) ([]Workload, error)

	// NewObject returns a new empty object for controller watches
	NewObject() client.Object
}
