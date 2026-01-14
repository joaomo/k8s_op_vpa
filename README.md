# VPA Operator

A Kubernetes operator that automatically creates Vertical Pod Autoscaler (VPA) resources for Deployments and StatefulSets.

## Description

The VPA Operator watches for Deployments and StatefulSets in your Kubernetes cluster and automatically creates VPA resources for them based on configuration. It also includes webhooks that create VPAs for new workloads and removes them when workloads are deleted.

## Features

- Automatically create VPA resources for Deployments and StatefulSets
- Filter workloads by namespace and workload labels
- Configure VPA update mode (Off, Initial, Auto)
- Set resource policies for containers
- Prometheus metrics for observability (RED principle)
- Structured logging
- Webhooks for handling Deployment and StatefulSet lifecycle events

## Releases

Container images are published to GitHub Container Registry for each release:

```sh
# Latest release
ghcr.io/joaomo/k8s_op_vpa:latest

# Specific version
ghcr.io/joaomo/k8s_op_vpa:0.1.0

# Major.minor version (tracks latest patch)
ghcr.io/joaomo/k8s_op_vpa:0.1
```

Images are built for both `linux/amd64` and `linux/arm64` architectures.

See [CHANGELOG.md](CHANGELOG.md) for release notes and version history.

## Getting Started

You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.

### Prerequisites

- Kubernetes cluster v1.25+
- kubectl configured to access your cluster
- [Vertical Pod Autoscaler CRDs](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler) installed in your cluster (the VPA controller is optional)

### Installation via Helm (Recommended)

```sh
# Install from local chart with ghcr.io image
helm install vpa-operator ./charts/vpa-operator -n vpa-operator-system --create-namespace \
  --set image.repository=ghcr.io/joaomo/k8s_op_vpa \
  --set image.tag=0.1.0

# With custom values
helm install vpa-operator ./charts/vpa-operator -n vpa-operator-system --create-namespace \
  --set image.repository=ghcr.io/joaomo/k8s_op_vpa \
  --set image.tag=0.1.0 \
  --set defaultVpaManager.enabled=true
```

### Installation via kubectl

1. Install the CRDs:

```sh
kubectl apply -f config/crd/bases/
```

2. Deploy the operator:

```sh
kubectl apply -f config/deploy/operator.yaml
```

3. Create a VpaManager instance:

```sh
kubectl apply -f config/samples/
```

### Configuration Options

The VpaManager custom resource supports the following configuration options:

```yaml
apiVersion: operators.joaomo.io/v1
kind: VpaManager
metadata:
  name: vpamanager-sample
spec:
  enabled: true                # Enable or disable the VPA operator
  updateMode: "Off"            # VPA update mode (Off, Initial, Auto)
  namespaceSelector:           # Label selector for namespaces to manage
    matchLabels:
      vpa-enabled: "true"
  deploymentSelector:          # Label selector for deployments to manage
    matchLabels:
      vpa-enabled: "true"
  statefulSetSelector:         # Label selector for statefulsets to manage
    matchLabels:
      vpa-enabled: "true"
  resourcePolicy:              # Resource policy for containers
    containerPolicies:
    - containerName: "*"       # Apply to all containers
      minAllowed:              # Minimum resources allowed
        cpu: "100m"
        memory: "100Mi"
      maxAllowed:              # Maximum resources allowed
        cpu: "1"
        memory: "1Gi"
```

2. Build and push your image to the location specified by `IMG`:

```sh
make docker-build docker-push IMG=<some-registry>/vpa-operator:tag
```

3. Deploy the controller to the cluster with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/vpa-operator:tag
```

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller
UnDeploy the controller from the cluster:

```sh
make undeploy
```

## Metrics

The operator exposes the following Prometheus metrics:

- `vpa_operator_reconcile_count`: Number of reconciliations performed
- `vpa_operator_reconcile_errors`: Number of errors encountered during reconciliation
- `vpa_operator_reconcile_duration_seconds`: Duration of reconciliation in seconds
- `vpa_operator_managed_vpas`: Number of VPAs managed by the operator
- `vpa_operator_watched_deployments`: Number of deployments watched by the operator
- `vpa_operator_webhook_requests_total`: Total number of webhook requests
- `vpa_operator_webhook_errors_total`: Total number of webhook errors
- `vpa_operator_webhook_duration_seconds`: Duration of webhook operations in seconds
- `vpa_operator_vpa_created_total`: Total number of VPAs created by the webhook
- `vpa_operator_vpa_deleted_total`: Total number of VPAs deleted by the webhook

## Contributing

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/),
which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster.

### Unit Tests

Run unit tests with:

```sh
make test
```

### E2E Validation

A comprehensive validation script is provided to test the operator against a real Kubernetes cluster:

```sh
# Build the operator image first
make docker-build IMG=vpa-operator:local

# Run validation against your current kubectl context
./hack/validate-operator.sh --image vpa-operator:local
```

**Options:**
- `--image <image>` - Operator image to use (default: `ghcr.io/joaomo/k8s_op_vpa:latest`)
- `--skip-cleanup` - Don't clean up resources after tests (useful for debugging)
- `--timeout <seconds>` - Timeout for wait operations (default: 120)

The validation script:
1. Installs VPA CRDs (not the full VPA controller)
2. Installs operator CRDs
3. Deploys the operator
4. Runs tests validating VPA creation, configuration, status updates, and cleanup
5. Cleans up all test resources

**CI/CD:** The script works in CI environments. A GitHub Actions workflow is provided in `.github/workflows/e2e.yml` that uses [kind](https://kind.sigs.k8s.io/) for testing.

### Local Development

1. Install the CRDs into the cluster:

```sh
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## Future Improvements

### Testing Gaps

- **VPA cleanup isolation**: The current cleanup test passes if either the operator's cleanup logic OR Kubernetes garbage collection (via owner references) deletes the VPA. A more robust test would verify the operator's `cleanupOrphanedVPAs()` function specifically.

- **resourcePolicy verification**: The E2E tests do not verify that VPA `containerPolicies` match the VpaManager spec's `resourcePolicy` configuration.

- **Webhook testing**: The deployment mutation webhook is disabled during E2E tests (requires TLS cert setup). Future work could add webhook-specific tests with cert-manager integration.

### Feature Ideas

- **DaemonSet support**: Extend to support DaemonSets
- **Dry-run mode**: Preview what VPAs would be created without actually creating them

> **Note**: VPA recommendations export was considered but would overlap with kube-state-metrics, which already provides `kube_vpa_*` metrics including recommendations. The operator's metrics follow the RED principle (Rate, Errors, Duration) and focus on operator-specific observability.

## License

Copyright 2025.

This software is licensed under the **Commons Clause License Condition v1.0** with **Apache License 2.0** as the underlying license.

**You may:**
- Use this software for personal, educational, or internal business purposes
- Modify and fork the software
- Redistribute with the same license terms

**You may not:**
- Sell the software or offer it as a paid service
- Use it as a key component of a commercial product for sale

See the [LICENSE](LICENSE) file for full details.

