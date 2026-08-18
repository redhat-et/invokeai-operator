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
	"fmt"

	corev1 "k8s.io/api/core/v1"

	invokeaiv1alpha1 "github.com/red-hat-et/invokeai-operator/api/v1alpha1"
)

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
