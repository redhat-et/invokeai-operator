/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InvokeAIPlatformSpec defines the desired state of InvokeAIPlatform.
type InvokeAIPlatformSpec struct {
	// InvokeAI configures the InvokeAI canvas/workflow engine deployment.
	InvokeAI InvokeAISpec `json:"invokeai"`

	// KServeMode specifies whether KServe is deployed as RawDeployment or Serverless (Knative).
	// This affects the predictor Service name and port used in backend URLs.
	// +kubebuilder:default="RawDeployment"
	// +optional
	KServeMode KServeMode `json:"kserveMode,omitempty"`

	// RuntimeImage is the vLLM-Omni container image used for operator-managed ServingRuntimes.
	// When set, the operator creates two ServingRuntimes per CR (vllm-multimodal and vllm-diffusion).
	// When omitted, the operator assumes ServingRuntimes already exist on the cluster.
	// +optional
	RuntimeImage string `json:"runtimeImage,omitempty"`

	// Backends is a list of inference backends the operator should deploy
	// as KServe InferenceServices and wire into InvokeAI.
	// +kubebuilder:validation:MinItems=1
	Backends []BackendSpec `json:"backends"`
}

// InvokeAISpec configures the InvokeAI Deployment and Service.
type InvokeAISpec struct {
	// Image is the container image for InvokeAI + bridge plugin.
	Image string `json:"image"`

	// Port is the port InvokeAI listens on.
	// +kubebuilder:default=9090
	// +optional
	Port int32 `json:"port,omitempty"`

	// Resources defines compute resource requirements for the InvokeAI container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// KServeMode specifies how KServe is deployed on the cluster,
// which determines the predictor Service naming and port convention.
// +kubebuilder:validation:Enum=RawDeployment;Serverless
type KServeMode string

const (
	KServeModeRawDeployment KServeMode = "RawDeployment"
	KServeModeServerless    KServeMode = "Serverless"
)

// BackendRole identifies a backend's function so the controller knows
// which environment variable to set on the InvokeAI Deployment.
// +kubebuilder:validation:Enum=reasoning;image-generation
type BackendRole string

const (
	BackendRoleReasoning       BackendRole = "reasoning"
	BackendRoleImageGeneration BackendRole = "image-generation"
)

// BackendSpec defines a single inference backend.
type BackendSpec struct {
	// Name is a unique identifier for this backend within the CR.
	// It becomes part of the InferenceService name.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`
	Name string `json:"name"`

	// Role tells the operator what this backend is for.
	// "reasoning" → operator sets VLLM_BASE_URL on the InvokeAI deployment.
	// "image-generation" → operator sets VLLM_IMAGE_BASE_URL.
	Role BackendRole `json:"role"`

	// Model is the HuggingFace model identifier.
	Model string `json:"model"`

	// Runtime is the name of the KServe ServingRuntime or ClusterServingRuntime.
	// +kubebuilder:default="vllm-multimodal"
	// +optional
	Runtime string `json:"runtime,omitempty"`

	// Resources defines compute resource requirements for the inference container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// ExtraArgs are additional arguments passed to the vLLM engine via the
	// InferenceService's args field.
	// +optional
	ExtraArgs []string `json:"extraArgs,omitempty"`
}

// InvokeAIPlatformStatus defines the observed state of InvokeAIPlatform.
type InvokeAIPlatformStatus struct {
	// Phase is the high-level summary of the platform state.
	// +kubebuilder:validation:Enum=Pending;Deploying;Ready;Degraded
	// +optional
	Phase PlatformPhase `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the
	// platform's current state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Backends reports the observed state of each inference backend.
	// +optional
	Backends []BackendStatus `json:"backends,omitempty"`
}

// PlatformPhase is the top-level lifecycle state.
// +kubebuilder:validation:Enum=Pending;Deploying;Ready;Degraded
type PlatformPhase string

const (
	PhasePending   PlatformPhase = "Pending"
	PhaseDeploying PlatformPhase = "Deploying"
	PhaseReady     PlatformPhase = "Ready"
	PhaseDegraded  PlatformPhase = "Degraded"
)

// BackendStatus reports the observed state of a single backend.
type BackendStatus struct {
	// Name matches the backend's spec.name.
	Name string `json:"name"`

	// Ready is true when the InferenceService predictor is serving.
	Ready bool `json:"ready"`

	// Model is the model currently loaded (echoed from spec for observability).
	// +optional
	Model string `json:"model,omitempty"`

	// URL is the in-cluster predictor URL the operator has wired into InvokeAI.
	// +optional
	URL string `json:"url,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// InvokeAIPlatform is the Schema for the invokeaiplatforms API.
type InvokeAIPlatform struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InvokeAIPlatformSpec   `json:"spec,omitempty"`
	Status InvokeAIPlatformStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InvokeAIPlatformList contains a list of InvokeAIPlatform.
type InvokeAIPlatformList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []InvokeAIPlatform `json:"items"`
}

func init() {
	SchemeBuilder.Register(&InvokeAIPlatform{}, &InvokeAIPlatformList{})
}
