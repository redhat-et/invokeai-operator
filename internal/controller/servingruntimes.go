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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kservev1alpha1 "github.com/kserve/kserve/pkg/apis/serving/v1alpha1"

	invokeaiv1alpha1 "github.com/red-hat-et/invokeai-operator/api/v1alpha1"
)

type runtimeDef struct {
	suffix  string
	command []string
	args    []string
}

var managedRuntimes = []runtimeDef{
	{
		suffix:  runtimeVLLMMultimodal,
		command: []string{"python", "-m", "vllm.entrypoints.openai.api_server"},
		args:    []string{"--model=/mnt/models", vllmPortArg},
	},
	{
		suffix:  "vllm-diffusion",
		command: []string{runtimeVLLM, "serve", "/mnt/models"},
		args:    []string{"--omni", vllmPortArg},
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
				labelName:      appName,
				labelInstance:  platform.Name,
				labelComponent: rd.suffix,
				labelManagedBy: operatorName,
			},
		},
		Spec: kservev1alpha1.ServingRuntimeSpec{
			SupportedModelFormats: []kservev1alpha1.SupportedModelFormat{
				{Name: runtimeVLLM, AutoSelect: ptr.To(true)},
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
							{Name: "LOGNAME", Value: runtimeVLLM},
							{Name: "USER", Value: runtimeVLLM},
						},
						Ports: []corev1.ContainerPort{
							{
								Name:          portHTTP,
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

func managedRuntimeName(platformName string, role invokeaiv1alpha1.BackendRole) string {
	switch role {
	case invokeaiv1alpha1.BackendRoleImageGeneration:
		return platformName + "-vllm-diffusion"
	default:
		return platformName + "-" + runtimeVLLMMultimodal
	}
}
