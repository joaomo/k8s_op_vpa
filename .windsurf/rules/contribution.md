# Contribution Guidelines for VPA Operator

When working on this project, always follow the guidelines in CONTRIBUTING.md.

## Code Changes

- Follow Go style guidelines and use `gofmt` for formatting
- Add comments for exported functions and types
- Keep functions focused and reasonably sized
- Always handle errors; do not ignore them
- Use structured logging with appropriate levels

## CRD and Type Changes

- When modifying Go types in `api/v1/`, always update the corresponding CRD YAML files:
  - `test/crds/vpamanager-crd.yaml`
  - `charts/vpa-operator/templates/crds/vpamanager-crd.yaml`
- Run `TestCRDSchemaMatchesGoTypes` to verify CRD and Go types are in sync:
  ```bash
  go test ./api/v1/... -run TestCRDSchemaMatchesGoTypes
  ```

## Testing Requirements

- Write or update tests for any code changes
- Ensure all tests pass before committing: `make test`
- Test error cases, not just happy paths
- Use table-driven tests where appropriate

## Commit Messages

Follow Conventional Commits format:
- `feat(<scope>): <description>` for new features
- `fix(<scope>): <description>` for bug fixes
- `docs(<scope>): <description>` for documentation
- `test(<scope>): <description>` for tests
- `refactor(<scope>): <description>` for refactoring

## Pull Request Checklist

Before submitting changes:
1. Run `make test` and `make lint`
2. Update documentation if needed (README.md, CHANGELOG.md)
3. Add or update tests for your changes
4. Ensure commit messages follow conventional commits format

## Directory Structure Reference

- `api/v1/` - API types and CRD schema tests
- `charts/` - Helm chart
- `config/` - Kubernetes manifests and samples
- `internal/controller/` - Reconciliation logic
- `internal/metrics/` - Prometheus metrics
- `internal/webhook/` - Admission webhooks
- `internal/workload/` - Workload abstractions
- `test/` - Test fixtures and CRDs
