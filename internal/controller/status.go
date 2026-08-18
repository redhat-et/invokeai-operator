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

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"

	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"

	invokeaiv1alpha1 "github.com/red-hat-et/invokeai-operator/api/v1alpha1"
)

func (r *InvokeAIPlatformReconciler) updateStatus(ctx context.Context, platform *invokeaiv1alpha1.InvokeAIPlatform) error {
	backendStatuses := make([]invokeaiv1alpha1.BackendStatus, 0, len(platform.Spec.Backends))
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
