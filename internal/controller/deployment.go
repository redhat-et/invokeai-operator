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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	invokeaiv1alpha1 "github.com/red-hat-et/invokeai-operator/api/v1alpha1"
)

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

	if deploymentSpecEqual(&existing, desired) && reflect.DeepEqual(existing.Labels, desired.Labels) {
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
							Name:      appName,
							Image:     platform.Spec.InvokeAI.Image,
							Resources: platform.Spec.InvokeAI.Resources,
							Ports: []corev1.ContainerPort{
								{
									Name:          portHTTP,
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

// deploymentSpecEqual compares only the container fields the operator controls.
// Kubernetes adds many defaulted fields (dnsPolicy, schedulerName,
// terminationGracePeriodSeconds, container terminationMessagePath, etc.) that
// would make a full reflect.DeepEqual always return false.
func deploymentSpecEqual(existing, desired *appsv1.Deployment) bool {
	if len(existing.Spec.Template.Spec.Containers) == 0 ||
		len(desired.Spec.Template.Spec.Containers) == 0 {
		return false
	}
	ec := existing.Spec.Template.Spec.Containers[0]
	dc := desired.Spec.Template.Spec.Containers[0]
	return ec.Image == dc.Image &&
		reflect.DeepEqual(ec.Resources, dc.Resources) &&
		reflect.DeepEqual(ec.Env, dc.Env) &&
		reflect.DeepEqual(ec.Ports, dc.Ports)
}
