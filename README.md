# invokeai-operator

A Kubernetes operator that deploys and manages the InvokeAI generative AI platform with pluggable vLLM inference backends.

## Description

The InvokeAI Operator automates the deployment and lifecycle management of an
[InvokeAI](https://github.com/invoke-ai/InvokeAI) instance backed by
[vLLM](https://github.com/vllm-project/vllm) /
[vLLM-Omni](https://github.com/vllm-project/vllm-omni) inference services on
Kubernetes and OpenShift.

A single `InvokeAIPlatform` custom resource replaces manual Helm installs and
`kubectl` wrangling. The operator's reconciliation loop continuously ensures the
desired state — creating Deployments, Services, and KServe InferenceServices,
wiring environment variables, and reporting per-backend health via the CR
status.

Key capabilities:

- **Declarative backend management** — list inference backends (reasoning,
  image generation) in the CR spec; the operator creates and configures the
  corresponding KServe InferenceServices.
- **Backend swapping** — change a model field and the operator handles the
  rollout: updates the InferenceService, waits for the new model to load, and
  re-wires InvokeAI automatically.
- **Self-healing** — accidentally deleted Deployments or InferenceServices are
  recreated within seconds.
- **Status reporting** — `status.phase` (Pending / Deploying / Ready /
  Degraded) and per-backend readiness give cluster admins a single pane of
  glass.

Built with the [Operator SDK](https://sdk.operatorframework.io/) (Go).
Companion project:
[invokeai-vllm-omni-bridge](https://github.com/redhat-et/invokeai-vllm-omni-bridge).

## Getting Started

### Prerequisites

- Go 1.24+
- Docker or Podman
- kubectl
- Access to a Kubernetes cluster with [KServe](https://kserve.github.io/website/) installed

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

Apply the sample CR:

```sh
kubectl apply -k config/samples/
```

### Uninstall

```sh
kubectl delete -k config/samples/
make uninstall
make undeploy
```

## License

Apache 2.0 — see [LICENSE](LICENSE).
