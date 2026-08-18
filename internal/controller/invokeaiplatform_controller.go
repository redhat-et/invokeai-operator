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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kservev1alpha1 "github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"

	invokeaiv1alpha1 "github.com/red-hat-et/invokeai-operator/api/v1alpha1"
)

const (
	labelName      = "app.kubernetes.io/name"
	labelInstance  = "app.kubernetes.io/instance"
	labelComponent = "app.kubernetes.io/component"
	labelManagedBy = "app.kubernetes.io/managed-by"

	runtimeVLLM           = "vllm"
	runtimeVLLMMultimodal = "vllm-multimodal"

	portHTTP    = "http"
	vllmPortArg = "--port=8000"

	appName      = "invokeai"
	operatorName = "invokeai-operator"
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
