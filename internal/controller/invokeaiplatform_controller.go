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

package controller

import (
	"context"
	"fmt"
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kservev1alpha1 "github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"

	invokeaiv1alpha1 "github.com/red-hat-et/invokeai-operator/api/v1alpha1"
)

// InvokeAIPlatformReconciler reconciles a InvokeAIPlatform object
type InvokeAIPlatformReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=invokeai.redhat.com,resources=invokeaiplatforms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=invokeai.redhat.com,resources=invokeaiplatforms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=invokeai.redhat.com,resources=invokeaiplatforms/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=serving.kserve.io,resources=inferenceservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=serving.kserve.io,resources=servingruntimes,verbs=get;list;watch;create;update;patch;delete

func (r *InvokeAIPlatformReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Step 1: Fetch the InvokeAIPlatform CR
	var platform invokeaiv1alpha1.InvokeAIPlatform
	if err := r.Get(ctx, req.NamespacedName, &platform); err != nil {
		if errors.IsNotFound(err) {
			log.Info("InvokeAIPlatform resource deleted, nothing to do")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Step 2: Reconcile operator-managed ServingRuntimes (if runtimeImage is set)
	if err := r.reconcileServingRuntimes(ctx, &platform); err != nil {
		return ctrl.Result{}, err
	}

	// Step 3: Reconcile each backend (InferenceService)
	for i := range platform.Spec.Backends {
		if err := r.reconcileBackend(ctx, &platform, &platform.Spec.Backends[i]); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Step 4: Delete orphaned InferenceServices
	if err := r.deleteOrphanedISVCs(ctx, &platform); err != nil {
		return ctrl.Result{}, err
	}

	// Step 5: Reconcile the InvokeAI Service
	if err := r.reconcileService(ctx, &platform); err != nil {
		return ctrl.Result{}, err
	}

	// Step 6: Reconcile the InvokeAI Deployment
	if err := r.reconcileDeployment(ctx, &platform); err != nil {
		return ctrl.Result{}, err
	}

	// Step 7: Update status
	if err := r.updateStatus(ctx, &platform); err != nil {
		return ctrl.Result{}, err
	}

	// Step 8: Requeue if not fully ready
	if platform.Status.Phase != invokeaiv1alpha1.PhaseReady {
		log.Info("Platform not fully ready, requeuing", "phase", platform.Status.Phase)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// --- Step 2: Reconcile operator-managed ServingRuntimes ---

type runtimeDef struct {
	suffix  string
	command []string
	args    []string
}

var managedRuntimes = []runtimeDef{
	{
		suffix:  "vllm-multimodal",
		command: []string{"python", "-m", "vllm.entrypoints.openai.api_server"},
		args:    []string{"--model=/mnt/models", "--port=8000"},
	},
	{
		suffix:  "vllm-diffusion",
		command: []string{"vllm", "serve", "/mnt/models"},
		args:    []string{"--omni", "--port=8000"},
	},
}

func (r *InvokeAIPlatformReconciler) reconcileServingRuntimes(ctx context.Context, platform *invokeaiv1alpha1.InvokeAIPlatform) error {
	if platform.Spec.RuntimeImage == "" {
		return nil
	}

	log := logf.FromContext(ctx)

	for _, rd := range managedRuntimes {
		name := platform.Name + "-" + rd.suffix
		desired := r.buildServingRuntime(platform, name, rd)
		if err := controllerutil.SetControllerReference(platform, desired, r.Scheme); err != nil {
			return fmt.Errorf("setting owner reference on ServingRuntime %s: %w", name, err)
		}

		var existing kservev1alpha1.ServingRuntime
		err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: platform.Namespace}, &existing)
		if errors.IsNotFound(err) {
			log.Info("Creating ServingRuntime", "name", name)
			if err := r.Create(ctx, desired); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}

		if reflect.DeepEqual(existing.Spec, desired.Spec) && reflect.DeepEqual(existing.Labels, desired.Labels) {
			continue
		}

		existing.Spec = desired.Spec
		existing.Labels = desired.Labels
		log.Info("Updating ServingRuntime", "name", name)
		if err := r.Update(ctx, &existing); err != nil {
			return err
		}
	}

	return nil
}

func (r *InvokeAIPlatformReconciler) buildServingRuntime(platform *invokeaiv1alpha1.InvokeAIPlatform, name string, rd runtimeDef) *kservev1alpha1.ServingRuntime {
	return &kservev1alpha1.ServingRuntime{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: platform.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "invokeai",
				"app.kubernetes.io/instance":   platform.Name,
				"app.kubernetes.io/component":  rd.suffix,
				"app.kubernetes.io/managed-by": "invokeai-operator",
			},
		},
		Spec: kservev1alpha1.ServingRuntimeSpec{
			SupportedModelFormats: []kservev1alpha1.SupportedModelFormat{
				{Name: "vllm", AutoSelect: ptr.To(true)},
			},
			MultiModel: ptr.To(false),
			ServingRuntimePodSpec: kservev1alpha1.ServingRuntimePodSpec{
				Containers: []corev1.Container{
					{
						Name:    "kserve-container",
						Image:   platform.Spec.RuntimeImage,
						Command: rd.command,
						Args:    rd.args,
						Env: []corev1.EnvVar{
							{Name: "HOME", Value: "/tmp"},
							{Name: "LOGNAME", Value: "vllm"},
							{Name: "USER", Value: "vllm"},
						},
						Ports: []corev1.ContainerPort{
							{
								Name:          "http",
								ContainerPort: 8000,
								Protocol:      corev1.ProtocolTCP,
							},
						},
					},
				},
			},
		},
	}
}

// --- Step 3: Reconcile a single backend as a KServe InferenceService ---

func (r *InvokeAIPlatformReconciler) reconcileBackend(ctx context.Context, platform *invokeaiv1alpha1.InvokeAIPlatform, backend *invokeaiv1alpha1.BackendSpec) error {
	log := logf.FromContext(ctx)
	isvcName := platform.Name + "-" + backend.Name

	desired := r.buildInferenceService(platform, backend)
	if err := controllerutil.SetControllerReference(platform, desired, r.Scheme); err != nil {
		return fmt.Errorf("setting owner reference on InferenceService %s: %w", isvcName, err)
	}

	var existing kservev1beta1.InferenceService
	err := r.Get(ctx, types.NamespacedName{Name: isvcName, Namespace: platform.Namespace}, &existing)
	if errors.IsNotFound(err) {
		log.Info("Creating InferenceService", "name", isvcName)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	if reflect.DeepEqual(existing.Spec, desired.Spec) && reflect.DeepEqual(existing.Labels, desired.Labels) {
		return nil
	}

	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	log.Info("Updating InferenceService", "name", isvcName)
	return r.Update(ctx, &existing)
}

func (r *InvokeAIPlatformReconciler) buildInferenceService(platform *invokeaiv1alpha1.InvokeAIPlatform, backend *invokeaiv1alpha1.BackendSpec) *kservev1beta1.InferenceService {
	isvcName := platform.Name + "-" + backend.Name
	runtime := backend.Runtime
	if platform.Spec.RuntimeImage != "" {
		runtime = managedRuntimeName(platform.Name, backend.Role)
	}
	storageURI := "hf://" + backend.Model

	isvc := &kservev1beta1.InferenceService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      isvcName,
			Namespace: platform.Namespace,
			Labels:    labelsForBackend(platform, backend.Name),
		},
		Spec: kservev1beta1.InferenceServiceSpec{
			Predictor: kservev1beta1.PredictorSpec{
				Model: &kservev1beta1.ModelSpec{
					ModelFormat: kservev1beta1.ModelFormat{Name: "vllm"},
					PredictorExtensionSpec: kservev1beta1.PredictorExtensionSpec{
						StorageURI: &storageURI,
						Container: corev1.Container{
							Resources: backend.Resources,
							Args:      backend.ExtraArgs,
						},
					},
				},
			},
		},
	}

	isvc.Spec.Predictor.Model.Runtime = &runtime

	return isvc
}

// --- Step 4: Delete orphaned InferenceServices ---

func (r *InvokeAIPlatformReconciler) deleteOrphanedISVCs(ctx context.Context, platform *invokeaiv1alpha1.InvokeAIPlatform) error {
	log := logf.FromContext(ctx)

	var isvcList kservev1beta1.InferenceServiceList
	if err := r.List(ctx, &isvcList, client.InNamespace(platform.Namespace), client.MatchingLabels{
		"app.kubernetes.io/managed-by": "invokeai-operator",
		"app.kubernetes.io/instance":   platform.Name,
	}); err != nil {
		return err
	}

	specNames := make(map[string]bool)
	for _, b := range platform.Spec.Backends {
		specNames[platform.Name+"-"+b.Name] = true
	}

	for i := range isvcList.Items {
		isvc := &isvcList.Items[i]
		if !specNames[isvc.Name] {
			log.Info("Deleting orphaned InferenceService", "name", isvc.Name)
			if err := r.Delete(ctx, isvc); err != nil && !errors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

// --- Step 5: Reconcile the InvokeAI Service ---

func (r *InvokeAIPlatformReconciler) reconcileService(ctx context.Context, platform *invokeaiv1alpha1.InvokeAIPlatform) error {
	log := logf.FromContext(ctx)
	svcName := platform.Name + "-invokeai"

	desired := r.buildService(platform)
	if err := controllerutil.SetControllerReference(platform, desired, r.Scheme); err != nil {
		return fmt.Errorf("setting owner reference on Service %s: %w", svcName, err)
	}

	var existing corev1.Service
	err := r.Get(ctx, types.NamespacedName{Name: svcName, Namespace: platform.Namespace}, &existing)
	if errors.IsNotFound(err) {
		log.Info("Creating Service", "name", svcName)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	if reflect.DeepEqual(existing.Spec.Ports, desired.Spec.Ports) &&
		reflect.DeepEqual(existing.Spec.Selector, desired.Spec.Selector) &&
		reflect.DeepEqual(existing.Labels, desired.Labels) {
		return nil
	}

	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.Selector = desired.Spec.Selector
	existing.Labels = desired.Labels
	log.Info("Updating Service", "name", svcName)
	return r.Update(ctx, &existing)
}

func (r *InvokeAIPlatformReconciler) buildService(platform *invokeaiv1alpha1.InvokeAIPlatform) *corev1.Service {
	svcName := platform.Name + "-invokeai"
	port := platform.Spec.InvokeAI.Port
	if port == 0 {
		port = 9090
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: platform.Namespace,
			Labels:    labelsForInvokeAI(platform),
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       port,
					TargetPort: intstr.FromInt32(port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Selector: selectorLabelsForInvokeAI(platform),
		},
	}
}

// --- Step 6: Reconcile the InvokeAI Deployment ---

func (r *InvokeAIPlatformReconciler) reconcileDeployment(ctx context.Context, platform *invokeaiv1alpha1.InvokeAIPlatform) error {
	log := logf.FromContext(ctx)
	deployName := platform.Name + "-invokeai"

	desired := r.buildDeployment(platform)
	if err := controllerutil.SetControllerReference(platform, desired, r.Scheme); err != nil {
		return fmt.Errorf("setting owner reference on Deployment %s: %w", deployName, err)
	}

	var existing appsv1.Deployment
	err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: platform.Namespace}, &existing)
	if errors.IsNotFound(err) {
		log.Info("Creating Deployment", "name", deployName)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	if reflect.DeepEqual(existing.Spec, desired.Spec) && reflect.DeepEqual(existing.Labels, desired.Labels) {
		return nil
	}

	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	log.Info("Updating Deployment", "name", deployName)
	return r.Update(ctx, &existing)
}

func (r *InvokeAIPlatformReconciler) buildDeployment(platform *invokeaiv1alpha1.InvokeAIPlatform) *appsv1.Deployment {
	deployName := platform.Name + "-invokeai"
	port := platform.Spec.InvokeAI.Port
	if port == 0 {
		port = 9090
	}

	envVars := r.deriveEnvVars(platform)
	labels := labelsForInvokeAI(platform)
	selectorLabels := selectorLabelsForInvokeAI(platform)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployName,
			Namespace: platform.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: selectorLabels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:      "invokeai",
							Image:     platform.Spec.InvokeAI.Image,
							Resources: platform.Spec.InvokeAI.Resources,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: port,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env: envVars,
						},
					},
				},
			},
		},
	}
}

func (r *InvokeAIPlatformReconciler) deriveEnvVars(platform *invokeaiv1alpha1.InvokeAIPlatform) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{Name: "VLLM_API_KEY", Value: "EMPTY"},
		{Name: "VLLM_TIMEOUT", Value: "120"},
	}

	for _, backend := range platform.Spec.Backends {
		url := predictorURL(platform.Name, backend.Name, platform.Namespace, platform.Spec.KServeMode)
		switch backend.Role {
		case invokeaiv1alpha1.BackendRoleReasoning:
			envVars = append(envVars, corev1.EnvVar{Name: "VLLM_BASE_URL", Value: url})
		case invokeaiv1alpha1.BackendRoleImageGeneration:
			envVars = append(envVars, corev1.EnvVar{Name: "VLLM_IMAGE_BASE_URL", Value: url})
		}
	}

	return envVars
}

func predictorURL(platformName, backendName, namespace string, mode invokeaiv1alpha1.KServeMode) string {
	if mode == invokeaiv1alpha1.KServeModeServerless {
		return fmt.Sprintf("http://%s-%s-predictor-default.%s.svc.cluster.local/v1",
			platformName, backendName, namespace)
	}
	return fmt.Sprintf("http://%s-%s-predictor.%s.svc.cluster.local:8000/v1",
		platformName, backendName, namespace)
}

func managedRuntimeName(platformName string, role invokeaiv1alpha1.BackendRole) string {
	switch role {
	case invokeaiv1alpha1.BackendRoleImageGeneration:
		return platformName + "-vllm-diffusion"
	default:
		return platformName + "-vllm-multimodal"
	}
}

// --- Step 7: Update status ---

func (r *InvokeAIPlatformReconciler) updateStatus(ctx context.Context, platform *invokeaiv1alpha1.InvokeAIPlatform) error {
	var backendStatuses []invokeaiv1alpha1.BackendStatus
	allBackendsReady := true

	for _, backend := range platform.Spec.Backends {
		isvcName := platform.Name + "-" + backend.Name
		var isvc kservev1beta1.InferenceService
		ready := false

		err := r.Get(ctx, types.NamespacedName{Name: isvcName, Namespace: platform.Namespace}, &isvc)
		if err == nil {
			ready = isInferenceServiceReady(&isvc)
		}

		if !ready {
			allBackendsReady = false
		}

		backendStatuses = append(backendStatuses, invokeaiv1alpha1.BackendStatus{
			Name:  backend.Name,
			Ready: ready,
			Model: backend.Model,
			URL:   predictorURL(platform.Name, backend.Name, platform.Namespace, platform.Spec.KServeMode),
		})
	}

	deployName := platform.Name + "-invokeai"
	var deploy appsv1.Deployment
	deployReady := false
	if err := r.Get(ctx, types.NamespacedName{Name: deployName, Namespace: platform.Namespace}, &deploy); err == nil {
		deployReady = deploy.Status.ReadyReplicas >= 1
	}

	platform.Status.Backends = backendStatuses

	switch {
	case allBackendsReady && deployReady:
		platform.Status.Phase = invokeaiv1alpha1.PhaseReady
	case !allBackendsReady && deployReady:
		platform.Status.Phase = invokeaiv1alpha1.PhaseDegraded
	case len(backendStatuses) > 0:
		platform.Status.Phase = invokeaiv1alpha1.PhaseDeploying
	default:
		platform.Status.Phase = invokeaiv1alpha1.PhasePending
	}

	return r.Status().Update(ctx, platform)
}

func isInferenceServiceReady(isvc *kservev1beta1.InferenceService) bool {
	for _, cond := range isvc.Status.Conditions {
		if cond.Type == "Ready" && cond.Status == "True" {
			return true
		}
	}
	return false
}

// --- Label helpers ---

func labelsForInvokeAI(platform *invokeaiv1alpha1.InvokeAIPlatform) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "invokeai",
		"app.kubernetes.io/instance":   platform.Name,
		"app.kubernetes.io/component":  "invokeai",
		"app.kubernetes.io/managed-by": "invokeai-operator",
	}
}

func selectorLabelsForInvokeAI(platform *invokeaiv1alpha1.InvokeAIPlatform) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "invokeai",
		"app.kubernetes.io/instance":  platform.Name,
		"app.kubernetes.io/component": "invokeai",
	}
}

func labelsForBackend(platform *invokeaiv1alpha1.InvokeAIPlatform, backendName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "invokeai",
		"app.kubernetes.io/instance":   platform.Name,
		"app.kubernetes.io/component":  backendName,
		"app.kubernetes.io/managed-by": "invokeai-operator",
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *InvokeAIPlatformReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&invokeaiv1alpha1.InvokeAIPlatform{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&kservev1alpha1.ServingRuntime{}).
		Owns(&kservev1beta1.InferenceService{}).
		Named("invokeaiplatform").
		Complete(r)
}
