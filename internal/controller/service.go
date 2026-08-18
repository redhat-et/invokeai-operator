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
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	invokeaiv1alpha1 "github.com/red-hat-et/invokeai-operator/api/v1alpha1"
)

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
					Name:       portHTTP,
					Port:       port,
					TargetPort: intstr.FromInt32(port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Selector: selectorLabelsForInvokeAI(platform),
		},
	}
}
