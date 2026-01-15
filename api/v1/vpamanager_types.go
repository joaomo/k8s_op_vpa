package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VpaManagerSpec defines the desired state of VpaManager
type VpaManagerSpec struct {
	// Enabled determines if the VPA operator is active
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// UpdateMode defines the VPA update mode (Off, Initial, Auto)
	// +kubebuilder:validation:Enum=Off;Initial;Auto
	// +kubebuilder:default="Off"
	UpdateMode string `json:"updateMode"`

	// NamespaceSelector selects the namespaces to manage VPAs for
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// DeploymentSelector selects the deployments to manage VPAs for
	// +optional
	DeploymentSelector *metav1.LabelSelector `json:"deploymentSelector,omitempty"`

	// StatefulSetSelector selects the statefulsets to manage VPAs for
	// +optional
	StatefulSetSelector *metav1.LabelSelector `json:"statefulSetSelector,omitempty"`

	// DaemonSetSelector selects the daemonsets to manage VPAs for
	// +optional
	DaemonSetSelector *metav1.LabelSelector `json:"daemonSetSelector,omitempty"`

	// ResourcePolicy defines the resource policy for the VPA
	// +optional
	ResourcePolicy *ResourcePolicy `json:"resourcePolicy,omitempty"`
}

// ResourcePolicy defines the resource policy for VPAs
type ResourcePolicy struct {
	// ContainerPolicies is a list of resource policies for containers
	ContainerPolicies []ContainerResourcePolicy `json:"containerPolicies,omitempty"`
}

// ContainerResourcePolicy defines the resource policy for a container
type ContainerResourcePolicy struct {
	// ContainerName is the name of the container
	ContainerName string `json:"containerName,omitempty"`

	// MinAllowed is the minimum amount of resources allowed
	MinAllowed map[string]string `json:"minAllowed,omitempty"`

	// MaxAllowed is the maximum amount of resources allowed
	MaxAllowed map[string]string `json:"maxAllowed,omitempty"`
}

// WorkloadReference contains information about a workload (Deployment, StatefulSet, or DaemonSet) with a VPA
type WorkloadReference struct {
	// Kind is the type of workload (Deployment or StatefulSet)
	Kind string `json:"kind"`

	// Name is the name of the workload
	Name string `json:"name"`

	// Namespace is the namespace of the workload
	Namespace string `json:"namespace"`

	// UID is the UID of the workload
	UID string `json:"uid"`

	// VpaName is the name of the VPA resource
	VpaName string `json:"vpaName"`
}

// DeploymentReference is an alias for backward compatibility
// Deprecated: Use WorkloadReference instead
type DeploymentReference = WorkloadReference

// VpaManagerStatus defines the observed state of VpaManager
type VpaManagerStatus struct {
	// ManagedVPAs is the total number of VPAs managed by this operator
	ManagedVPAs int `json:"managedVPAs"`

	// ManagedDeployments is a list of deployments that have VPAs
	// Deprecated: Use ManagedWorkloads instead. Will be removed in v1.
	// +optional
	ManagedDeployments []WorkloadReference `json:"managedDeployments,omitempty"`

	// ManagedWorkloads is a list of all workloads that have VPAs
	// Deprecated: This field is expensive at scale. Use count fields instead.
	// +optional
	ManagedWorkloads []WorkloadReference `json:"managedWorkloads,omitempty"`

	// DeploymentCount is the number of deployments with managed VPAs
	DeploymentCount int `json:"deploymentCount,omitempty"`

	// StatefulSetCount is the number of statefulsets with managed VPAs
	StatefulSetCount int `json:"statefulSetCount,omitempty"`

	// DaemonSetCount is the number of daemonsets with managed VPAs
	DaemonSetCount int `json:"daemonSetCount,omitempty"`

	// LastReconcileTime is the last time the operator reconciled
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=vpa
// +kubebuilder:printcolumn:name="Enabled",type="boolean",JSONPath=".spec.enabled"
// +kubebuilder:printcolumn:name="UpdateMode",type="string",JSONPath=".spec.updateMode"
// +kubebuilder:printcolumn:name="ManagedVPAs",type="integer",JSONPath=".status.managedVPAs"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// VpaManager is the Schema for the vpamanagers API
type VpaManager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VpaManagerSpec   `json:"spec,omitempty"`
	Status VpaManagerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VpaManagerList contains a list of VpaManager
type VpaManagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VpaManager `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VpaManager{}, &VpaManagerList{})
}
