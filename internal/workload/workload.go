package workload

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PageSize is the default number of items to fetch per page
const PageSize = 500

// Workload abstracts Deployment, StatefulSet, DaemonSet for VPA management
type Workload interface {
	GetName() string
	GetNamespace() string
	GetUID() types.UID
	GetLabels() map[string]string
	GetKind() string
	GetAPIVersion() string
}

// WorkloadCallback is called for each workload during iteration
// Return false to stop iteration, or an error to abort with error
type WorkloadCallback func(Workload) (continueIteration bool, err error)

// Provider lists and matches workloads of a specific type
type Provider interface {
	// Kind returns the workload kind (e.g., "Deployment", "StatefulSet", "DaemonSet")
	Kind() string

	// List returns all workloads in a namespace matching the selector
	// Deprecated: Use ForEach for better memory efficiency with large datasets
	List(ctx context.Context, c client.Client, namespace string, selector *metav1.LabelSelector) ([]Workload, error)

	// ForEach iterates over workloads with pagination, calling the callback for each
	// This is more memory-efficient than List for large datasets
	ForEach(ctx context.Context, c client.Client, namespace string, selector *metav1.LabelSelector, callback WorkloadCallback) error

	// NewObject returns a new empty object for controller watches
	NewObject() client.Object
}
