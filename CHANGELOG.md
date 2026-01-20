# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.1] - 2026-01-20

### Added
- Detailed [CONTRIBUTING.md](CONTRIBUTING.md) guide and Windsurf contribution rules to standardize community workflows
- Regression test (`TestCRDSchemaMatchesGoTypes`) that validates CRD schemas stay in sync with Go API types

### Changed
- Helm chart exposes zap logging flags (`logging.level`, `logging.development`, encoder, stacktrace level) so operators can tune verbosity without rebuilding images
- Default logger now starts in production mode (no implicit development verbosity)
- CRD definitions now include every field referenced by the controller (selectors, workload counts, workload references) to prevent API warnings

## [0.2.0] - 2026-01-15

### Changed
- Status updates now use `Patch` instead of `Update` to avoid race conditions with stale `resourceVersion`
- **Performance**: Workload iteration now uses pagination (500 items per page) to reduce memory usage
- **Performance**: Status now stores counts by workload type instead of full workload list (prevents etcd size limits)
- **Performance**: VPA updates use hash-based change detection to skip unnecessary API calls

### Fixed
- VPA owner references now include `Controller: true` and `BlockOwnerDeletion: true` for proper Kubernetes garbage collection
- Improved Helm chart extensibility with support for `commonLabels`, `commonAnnotations`, `podLabels`, and `podAnnotations`
- Added missing `statefulSetSelector` and `daemonSetSelector` to `defaultVpaManager` Helm values

### Added
- E2E test for validating VPA owner reference configuration
- GitHub Container Registry (GHCR) support in CI/CD workflows
- Helm chart publishing to GHCR OCI registry (`oci://ghcr.io/joaomo/charts/vpa-operator`)

## [0.1.0] - 2026-01-14

### Added
- Initial release of VPA Operator
- VpaManager custom resource for configuring automatic VPA creation
- Namespace, deployment, statefulset and daemonset label selectors for filtering targets
- Configurable VPA update modes (Off, Initial, Auto)
- Resource policies with min/max allowed settings
- Prometheus metrics for observability
- Deployment webhook for lifecycle events
- E2E validation script (`hack/validate-operator.sh`)
- GitHub Actions CI/CD workflows for testing and releases
- Helm chart for installation
- Multi-architecture Docker image support (amd64/arm64)

### Security
- Container runs as non-root user (UID 65532)
- Read-only root filesystem
- Dropped all Linux capabilities
- Security context with `allowPrivilegeEscalation: false`

[Unreleased]: https://github.com/joaomo/k8s_op_vpa/compare/v0.2.1...HEAD
[0.2.1]: https://github.com/joaomo/k8s_op_vpa/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/joaomo/k8s_op_vpa/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/joaomo/k8s_op_vpa/releases/tag/v0.1.0
