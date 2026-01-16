# Contributing to VPA Operator

Thank you for your interest in contributing to the VPA Operator! This document provides guidelines and information for contributors.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [How to Contribute](#how-to-contribute)
- [Development Setup](#development-setup)
- [Pull Request Process](#pull-request-process)
- [Coding Guidelines](#coding-guidelines)
- [Testing](#testing)
- [Documentation](#documentation)

## Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](https://www.contributor-covenant.org/version/2/1/code_of_conduct/). By participating, you are expected to uphold this code. Please report unacceptable behavior to the project maintainers.

## Getting Started

1. Fork the repository on GitHub
2. Clone your fork locally
3. Set up the development environment (see [Development Setup](#development-setup))
4. Create a branch for your changes

## How to Contribute

### Reporting Bugs

Before submitting a bug report:

- Check the [existing issues](https://github.com/joaomo/k8s_op_vpa/issues) to avoid duplicates
- Collect information about the bug:
  - Stack trace (if applicable)
  - OS, platform, and version
  - Kubernetes version
  - VPA Operator version
  - Steps to reproduce

When filing an issue, use the bug report template and include:

- A clear, descriptive title
- Detailed steps to reproduce the issue
- Expected vs. actual behavior
- Any relevant logs or screenshots

### Suggesting Features

Feature requests are welcome! Please:

- Check existing issues and pull requests first
- Open an issue with the "enhancement" label
- Clearly describe the feature and its use case
- Explain why this feature would be useful to other users

### Submitting Code Changes

1. Create a new branch from `main`:
   ```bash
   git checkout -b feature/your-feature-name
   # or
   git checkout -b fix/your-bug-fix
   ```

2. Make your changes following our [Coding Guidelines](#coding-guidelines)

3. Write or update tests as needed

4. Ensure all tests pass:
   ```bash
   make test
   ```

5. Commit your changes with a clear commit message:
   ```bash
   git commit -m "Add feature: brief description"
   ```

6. Push to your fork and submit a pull request

## Development Setup

### Prerequisites

- Go 1.21+
- Docker
- kubectl
- A Kubernetes cluster (kind, minikube, or remote)
- [controller-gen](https://book.kubebuilder.io/reference/controller-gen.html) for CRD generation

### Building

```bash
# Build the operator binary
make build

# Build the Docker image
make docker-build IMG=your-registry/vpa-operator:tag

# Run tests
make test

# Run linting
make lint
```

### Running Locally

```bash
# Install CRDs
make install

# Run the operator locally (outside the cluster)
make run

# Or deploy to a cluster
make deploy IMG=your-registry/vpa-operator:tag
```

## Pull Request Process

1. **Before submitting:**
   - Ensure your code follows the project's coding standards
   - Run `make test` and `make lint`
   - Update documentation if needed
   - Add or update tests for your changes

2. **PR requirements:**
   - Clear description of what the PR does
   - Reference any related issues (e.g., "Fixes #123")
   - All CI checks must pass
   - At least one maintainer approval required

3. **After submitting:**
   - Respond to review feedback promptly
   - Make requested changes in new commits (don't force-push during review)
   - Squash commits when ready to merge (if requested)

### Commit Message Guidelines

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

[optional body]

[optional footer(s)]
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code changes that neither fix bugs nor add features
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

Examples:
```
feat(controller): add support for DaemonSet workloads
fix(webhook): handle nil pointer in deployment mutation
docs(readme): update installation instructions
```

## Coding Guidelines

### Go Style

- Follow the [Effective Go](https://golang.org/doc/effective_go.html) guidelines
- Use `gofmt` for formatting (enforced by CI)
- Follow [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- Keep functions focused and reasonably sized
- Add comments for exported functions and types

### Project-Specific Guidelines

- **Error handling:** Always handle errors; don't ignore them
- **Logging:** Use structured logging with appropriate levels
- **Tests:** Maintain or improve test coverage
- **CRD changes:** Update both Go types and CRD YAML; run `TestCRDSchemaMatchesGoTypes`
- **Metrics:** Add Prometheus metrics for observable operations

### Directory Structure

```
├── api/v1/              # API types and CRD schema tests
├── charts/              # Helm chart
├── config/              # Kubernetes manifests and samples
├── internal/
│   ├── controller/      # Reconciliation logic
│   ├── metrics/         # Prometheus metrics
│   ├── webhook/         # Admission webhooks
│   └── workload/        # Workload abstractions
├── test/                # Test fixtures and CRDs
└── main.go              # Entry point
```

## Testing

### Running Tests

```bash
# Unit tests
make test

# Specific package
go test ./internal/controller/...

# With verbose output
go test -v ./...

# CRD schema validation
go test ./api/v1/... -run TestCRDSchemaMatchesGoTypes
```

### Writing Tests

- Use table-driven tests where appropriate
- Mock external dependencies
- Test error cases, not just happy paths
- Use `envtest` for controller tests

### End-to-End Tests

```bash
# Run e2e tests (requires a cluster)
make test-e2e
```

## Documentation

- Update the README.md for user-facing changes
- Update CHANGELOG.md for notable changes
- Add inline code comments for complex logic
- Update Helm chart values.yaml comments for new options

## Questions?

If you have questions, feel free to:

- Open a [GitHub Discussion](https://github.com/joaomo/k8s_op_vpa/discussions)
- Ask in an issue

Thank you for contributing!
