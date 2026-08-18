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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"

	invokeaiv1alpha1 "github.com/red-hat-et/invokeai-operator/api/v1alpha1"
)

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

	if isvcModelSpecEqual(&existing, desired) && reflect.DeepEqual(existing.Labels, desired.Labels) {
		return nil
	}

	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	log.Info("Updating InferenceService", "name", isvcName)
	return r.Update(ctx, &existing)
}

func (r *InvokeAIPlatformReconciler) buildInferenceService(platform *invokeaiv1alpha1.InvokeAIPlatform, backend *invokeaiv1alpha1.BackendSpec) *kservev1beta1.InferenceService {
	isvcName := platform.Name + "-" + backend.Name
	runtimeName := backend.Runtime
	if platform.Spec.RuntimeImage != "" {
		runtimeName = managedRuntimeName(platform.Name, backend.Role)
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
					ModelFormat: kservev1beta1.ModelFormat{Name: runtimeVLLM},
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

	isvc.Spec.Predictor.Model.Runtime = &runtimeName

	return isvc
}

// isvcModelSpecEqual compares only the fields the operator controls in the
// InferenceService spec. KServe's webhook adds extra fields (e.g.
// automountServiceAccountToken) that would make a full reflect.DeepEqual
// always return false and trigger a spurious update on every reconcile.
func isvcModelSpecEqual(existing, desired *kservev1beta1.InferenceService) bool {
	em := existing.Spec.Predictor.Model
	dm := desired.Spec.Predictor.Model
	if em == nil || dm == nil {
		return em == dm
	}
	return reflect.DeepEqual(em.StorageURI, dm.StorageURI) &&
		reflect.DeepEqual(em.Runtime, dm.Runtime) &&
		reflect.DeepEqual(em.Resources, dm.Resources) &&
		reflect.DeepEqual(em.Args, dm.Args) &&
		reflect.DeepEqual(em.ModelFormat, dm.ModelFormat)
}

func (r *InvokeAIPlatformReconciler) deleteOrphanedISVCs(ctx context.Context, platform *invokeaiv1alpha1.InvokeAIPlatform) error {
	log := logf.FromContext(ctx)

	var isvcList kservev1beta1.InferenceServiceList
	if err := r.List(ctx, &isvcList, client.InNamespace(platform.Namespace), client.MatchingLabels{
		labelManagedBy: operatorName,
		labelInstance:  platform.Name,
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
