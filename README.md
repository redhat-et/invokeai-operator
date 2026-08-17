# invokeai-operator

A Kubernetes operator for deploying and managing [InvokeAI](https://github.com/invoke-ai/InvokeAI) on OpenShift and Kubernetes, with optional KServe inference backend wiring.

## Description

The `InvokeAIPlatform` custom resource gives you a single declarative spec for an InvokeAI deployment. The operator continuously reconciles the cluster to match it.

At minimum, a CR with only `spec.invokeai` gives you a managed InvokeAI Deployment and ClusterIP Service. Adding `spec.backends` extends this to KServe `InferenceService` management and automatic environment variable wiring so InvokeAI can reach the inference backends.

Capabilities:

- **InvokeAI lifecycle management.** Creates and maintains the InvokeAI Deployment and ClusterIP Service. Accidental deletions or spec drift are corrected within seconds.
- **Optional KServe backend wiring.** When `spec.backends` is populated, the operator creates one `InferenceService` per backend and sets `VLLM_BASE_URL` or `VLLM_IMAGE_BASE_URL` on the InvokeAI Deployment based on each backend's role. Backends removed from the spec are cleaned up automatically.
- **Operator-managed ServingRuntimes.** When `spec.runtimeImage` is set, the operator creates `vllm-multimodal` and `vllm-diffusion` `ServingRuntime` resources automatically. When omitted, the operator assumes runtimes already exist on the cluster.
- **Model swapping.** Changing `spec.backends[].model` updates the InferenceService in place. The operator reflects the transition in `status.phase` while the new model loads.
- **Status reporting.** `status.phase` (Pending, Deploying, Ready, Degraded) and `status.backends` expose per-backend readiness and the wired predictor URL.

Built with the [Operator SDK](https://sdk.operatorframework.io/) (Go). Validated on Red Hat OpenShift AI 3.4.3 with KServe in RawDeployment mode.

Companion project: [invokeai-vllm-omni-bridge](https://github.com/redhat-et/invokeai-vllm-omni-bridge).

## Getting Started

### Prerequisites

- Go 1.25+
- Docker or Podman
- kubectl or oc
- A Kubernetes or OpenShift cluster with KServe installed (Red Hat OpenShift AI 2.x+ recommended)

### Run locally against a cluster

The fastest way to test is to run the operator locally, pointed at a remote cluster:

```sh
make install        # install CRDs into the cluster
go run ./cmd/main.go
```

### Deploy on the cluster

Build and push the operator image:

```sh
make docker-build docker-push IMG=<your-registry>/invokeai-operator:tag
```

Install the CRDs and deploy the controller:

```sh
make install
make deploy IMG=<your-registry>/invokeai-operator:tag
```

### Sample CR

Minimal setup, InvokeAI only with no KServe backends:

```yaml
apiVersion: invokeai.redhat.com/v1alpha1
kind: InvokeAIPlatform
metadata:
  name: my-studio
  namespace: ai-workloads
spec:
  invokeai:
    image: invoke-ai/invokeai:latest
    port: 9090
```

Full setup with operator-managed ServingRuntimes and two backends (requires the [bridge image](https://github.com/redhat-et/invokeai-vllm-omni-bridge)):

```yaml
apiVersion: invokeai.redhat.com/v1alpha1
kind: InvokeAIPlatform
metadata:
  name: my-studio
  namespace: ai-workloads
spec:
  invokeai:
    image: quay.io/redhat-et/invokeai-vllm-omni-bridge:latest
    port: 9090
  kserveMode: RawDeployment
  runtimeImage: docker.io/vllm/vllm-omni:v0.22.0
  backends:
    - name: reasoning
      role: reasoning
      model: Qwen/Qwen2.5-Omni-7B
      resources:
        requests: {nvidia.com/gpu: "1", memory: "24Gi", cpu: "4"}
        limits:   {nvidia.com/gpu: "1", memory: "32Gi", cpu: "8"}
    - name: image-generation
      role: image-generation
      model: black-forest-labs/FLUX.2-klein-4B
      resources:
        requests: {nvidia.com/gpu: "1", memory: "16Gi", cpu: "2"}
        limits:   {nvidia.com/gpu: "1", memory: "24Gi", cpu: "4"}
```

### Uninstall

```sh
kubectl delete -k config/samples/
make uninstall
make undeploy
```

## License

Apache 2.0. See [LICENSE](LICENSE).
