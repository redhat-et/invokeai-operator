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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kservev1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"

	invokeaiv1alpha1 "github.com/red-hat-et/invokeai-operator/api/v1alpha1"
)

var _ = Describe("InvokeAIPlatform Controller", func() {
	const (
		platformName = "test-studio"
		namespace    = "default"
	)

	ctx := context.Background()
	namespacedName := types.NamespacedName{Name: platformName, Namespace: namespace}

	newPlatform := func() *invokeaiv1alpha1.InvokeAIPlatform {
		return &invokeaiv1alpha1.InvokeAIPlatform{
			ObjectMeta: metav1.ObjectMeta{
				Name:      platformName,
				Namespace: namespace,
			},
			Spec: invokeaiv1alpha1.InvokeAIPlatformSpec{
				InvokeAI: invokeaiv1alpha1.InvokeAISpec{
					Image: "ghcr.io/redhat-et/invokeai-vllm-omni-bridge:latest",
					Port:  9090,
				},
				Backends: []invokeaiv1alpha1.BackendSpec{
					{
						Name:    "reasoning",
						Role:    invokeaiv1alpha1.BackendRoleReasoning,
						Model:   "Qwen/Qwen2.5-Omni-7B",
						Runtime: "vllm-multimodal",
					},
					{
						Name:    "image-generation",
						Role:    invokeaiv1alpha1.BackendRoleImageGeneration,
						Model:   "black-forest-labs/FLUX.2-klein-4B",
						Runtime: "vllm-multimodal",
					},
				},
			},
		}
	}

	doReconcile := func() (reconcile.Result, error) {
		r := &InvokeAIPlatformReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
		return r.Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
	}

	AfterEach(func() {
		// Clean up the CR (cascading delete removes children via owner refs)
		platform := &invokeaiv1alpha1.InvokeAIPlatform{}
		if err := k8sClient.Get(ctx, namespacedName, platform); err == nil {
			Expect(k8sClient.Delete(ctx, platform)).To(Succeed())
		}
	})

	Context("When a new InvokeAIPlatform CR is created", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, newPlatform())).To(Succeed())
		})

		It("should create InferenceServices for each backend", func() {
			_, err := doReconcile()
			Expect(err).NotTo(HaveOccurred())

			var reasoningISVC kservev1beta1.InferenceService
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: platformName + "-reasoning", Namespace: namespace,
			}, &reasoningISVC)).To(Succeed())
			Expect(*reasoningISVC.Spec.Predictor.Model.StorageURI).To(Equal("hf://Qwen/Qwen2.5-Omni-7B"))
			Expect(*reasoningISVC.Spec.Predictor.Model.Runtime).To(Equal("vllm-multimodal"))

			var imagegenISVC kservev1beta1.InferenceService
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: platformName + "-image-generation", Namespace: namespace,
			}, &imagegenISVC)).To(Succeed())
			Expect(*imagegenISVC.Spec.Predictor.Model.StorageURI).To(Equal("hf://black-forest-labs/FLUX.2-klein-4B"))
		})

		It("should create a ClusterIP Service", func() {
			_, err := doReconcile()
			Expect(err).NotTo(HaveOccurred())

			var svc corev1.Service
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: platformName + "-invokeai", Namespace: namespace,
			}, &svc)).To(Succeed())
			Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(9090)))
		})

		It("should create a Deployment with correct env vars", func() {
			_, err := doReconcile()
			Expect(err).NotTo(HaveOccurred())

			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: platformName + "-invokeai", Namespace: namespace,
			}, &deploy)).To(Succeed())

			Expect(deploy.Spec.Template.Spec.Containers).To(HaveLen(1))
			container := deploy.Spec.Template.Spec.Containers[0]
			Expect(container.Image).To(Equal("ghcr.io/redhat-et/invokeai-vllm-omni-bridge:latest"))

			envMap := make(map[string]string)
			for _, e := range container.Env {
				envMap[e.Name] = e.Value
			}
			Expect(envMap).To(HaveKeyWithValue("VLLM_API_KEY", "EMPTY"))
			Expect(envMap).To(HaveKeyWithValue("VLLM_TIMEOUT", "120"))
			Expect(envMap).To(HaveKey("VLLM_BASE_URL"))
			Expect(envMap["VLLM_BASE_URL"]).To(ContainSubstring(platformName + "-reasoning-predictor."))
			Expect(envMap).To(HaveKey("VLLM_IMAGE_BASE_URL"))
			Expect(envMap["VLLM_IMAGE_BASE_URL"]).To(ContainSubstring(platformName + "-image-generation-predictor."))
		})

		It("should set status phase to Deploying (backends not yet ready)", func() {
			_, err := doReconcile()
			Expect(err).NotTo(HaveOccurred())

			var platform invokeaiv1alpha1.InvokeAIPlatform
			Expect(k8sClient.Get(ctx, namespacedName, &platform)).To(Succeed())
			Expect(platform.Status.Phase).To(Equal(invokeaiv1alpha1.PhaseDeploying))
			Expect(platform.Status.Backends).To(HaveLen(2))
		})

		It("should requeue when not fully ready", func() {
			result, err := doReconcile()
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))
		})
	})

	Context("When a backend is removed from spec", func() {
		It("should delete the orphaned InferenceService", func() {
			// Create with two backends
			Expect(k8sClient.Create(ctx, newPlatform())).To(Succeed())
			_, err := doReconcile()
			Expect(err).NotTo(HaveOccurred())

			// Verify both ISVCs exist
			var isvc kservev1beta1.InferenceService
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: platformName + "-image-generation", Namespace: namespace,
			}, &isvc)).To(Succeed())

			// Remove image-generation backend from spec
			var platform invokeaiv1alpha1.InvokeAIPlatform
			Expect(k8sClient.Get(ctx, namespacedName, &platform)).To(Succeed())
			platform.Spec.Backends = platform.Spec.Backends[:1] // keep only reasoning
			Expect(k8sClient.Update(ctx, &platform)).To(Succeed())

			// Reconcile again
			_, err = doReconcile()
			Expect(err).NotTo(HaveOccurred())

			// The orphaned ISVC should be deleted
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: platformName + "-image-generation", Namespace: namespace,
			}, &isvc)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

	Context("When the CR is deleted", func() {
		It("should reconcile without error", func() {
			// Reconcile a non-existent resource — should be a no-op
			result, err := doReconcile()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})
})
