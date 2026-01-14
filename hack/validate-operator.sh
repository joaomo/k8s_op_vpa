#!/usr/bin/env bash
#
# VPA Operator Validation Script
#
# This script validates the behavior of the VPA operator against a Kubernetes cluster.
# It can be run locally or in CI/CD pipelines provided kubectl is configured.
#
# Requirements:
#   - kubectl configured with access to a Kubernetes cluster
#   - Cluster admin permissions (for CRD installation)
#
# Usage:
#   ./hack/validate-operator.sh [options]
#
# Options:
#   --skip-build      Skip building the operator image
#   --skip-cleanup    Skip cleanup after tests (useful for debugging)
#   --image           Operator image to use (default: ghcr.io/joaomo/k8s_op_vpa:latest)
#   --timeout         Timeout for wait operations in seconds (default: 120)
#   --help            Show this help message
#

set -euo pipefail

# Colors for output (disabled in CI)
if [[ -t 1 ]] && [[ -z "${CI:-}" ]]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    NC='\033[0m' # No Color
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    NC=''
fi

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
TEST_NAMESPACE="vpa-operator-test"
HELM_RELEASE_NAME="vpa-operator"
HELM_NAMESPACE="vpa-operator-system"
OPERATOR_NAMESPACE="${HELM_NAMESPACE}"
SKIP_BUILD="${SKIP_BUILD:-false}"
SKIP_CLEANUP="${SKIP_CLEANUP:-false}"
OPERATOR_IMAGE="${OPERATOR_IMAGE:-ghcr.io/joaomo/k8s_op_vpa:latest}"
TIMEOUT="${TIMEOUT:-120}"

# Test counters
TESTS_PASSED=0
TESTS_FAILED=0

#######################################
# Logging functions
#######################################
log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[PASS]${NC} $*"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

log_error() {
    echo -e "${RED}[FAIL]${NC} $*"
}

log_step() {
    echo -e "\n${BLUE}==>${NC} $*"
}

#######################################
# Parse command line arguments
#######################################
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --skip-build)
                SKIP_BUILD=true
                shift
                ;;
            --skip-cleanup)
                SKIP_CLEANUP=true
                shift
                ;;
            --image)
                OPERATOR_IMAGE="$2"
                shift 2
                ;;
            --timeout)
                TIMEOUT="$2"
                shift 2
                ;;
            --help|-h)
                head -30 "$0" | tail -25
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                exit 1
                ;;
        esac
    done
}

#######################################
# Check prerequisites
#######################################
check_prerequisites() {
    log_step "Checking prerequisites"

    # Check kubectl
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed"
        exit 1
    fi
    log_info "kubectl found: $(kubectl version --client --short 2>/dev/null || kubectl version --client -o yaml | grep gitVersion | head -1)"

    # Check cluster connectivity
    if ! kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster. Please check your kubeconfig."
        exit 1
    fi
    log_info "Connected to cluster: $(kubectl config current-context)"

    # Check if we have admin permissions (needed for CRDs)
    if ! kubectl auth can-i create customresourcedefinitions &> /dev/null; then
        log_warn "May not have permissions to create CRDs - tests might fail"
    fi

    # Check helm (required for deployment)
    if ! command -v helm &> /dev/null; then
        log_error "helm is not installed"
        exit 1
    fi
    log_info "helm found: $(helm version --short)"

    log_success "Prerequisites check passed"
}

#######################################
# Deploy operator via Helm
#######################################
deploy_operator() {
    log_step "Deploying operator via Helm"

    # Parse image repository and tag from OPERATOR_IMAGE
    local image_repo="${OPERATOR_IMAGE%:*}"
    local image_tag="${OPERATOR_IMAGE##*:}"
    
    # If no tag specified, default to latest
    if [[ "${image_repo}" == "${image_tag}" ]]; then
        image_tag="latest"
    fi

    log_info "Deploying operator with Helm..."
    log_info "  Image: ${image_repo}:${image_tag}"
    
    helm upgrade --install "${HELM_RELEASE_NAME}" "${PROJECT_ROOT}/charts/vpa-operator" \
        --namespace "${HELM_NAMESPACE}" \
        --create-namespace \
        --set image.repository="${image_repo}" \
        --set image.tag="${image_tag}" \
        --set image.pullPolicy=IfNotPresent \
        --set crds.install=true \
        --set leaderElection.enabled=false \
        --wait \
        --timeout "${TIMEOUT}s"

    log_success "Operator deployed via Helm"
}

#######################################
# Create test resources
#######################################
create_test_resources() {
    log_step "Creating test resources"

    # Create test namespace with label for VPA selection
    log_info "Creating test namespace..."
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: ${TEST_NAMESPACE}
  labels:
    vpa-enabled: "true"
EOF

    # Create test deployment
    log_info "Creating test deployment..."
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
  namespace: ${TEST_NAMESPACE}
  labels:
    app: test-app
    vpa-enabled: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test-app
  template:
    metadata:
      labels:
        app: test-app
    spec:
      containers:
      - name: nginx
        image: nginx:alpine
        resources:
          requests:
            cpu: 10m
            memory: 32Mi
          limits:
            cpu: 100m
            memory: 128Mi
EOF

    # Wait for deployment to be ready
    kubectl rollout status deployment/test-app -n "${TEST_NAMESPACE}" --timeout="${TIMEOUT}s"

    log_success "Test resources created"
}

#######################################
# Test: VPA creation
#######################################
test_vpa_creation() {
    log_step "Test: VPA creation for matching deployments"

    # Create VpaManager
    log_info "Creating VpaManager..."
    cat <<EOF | kubectl apply -f -
apiVersion: operators.joaomo.io/v1
kind: VpaManager
metadata:
  name: test-vpamanager
spec:
  enabled: true
  updateMode: "Off"
  namespaceSelector:
    matchLabels:
      vpa-enabled: "true"
  deploymentSelector:
    matchLabels:
      vpa-enabled: "true"
  resourcePolicy:
    containerPolicies:
    - containerName: "*"
      minAllowed:
        cpu: "10m"
        memory: "32Mi"
      maxAllowed:
        cpu: "1"
        memory: "1Gi"
EOF

    # Wait for VPA to be created
    log_info "Waiting for VPA to be created..."
    local retries=0
    local max_retries=30
    while [[ $retries -lt $max_retries ]]; do
        if kubectl get vpa test-app-vpa -n "${TEST_NAMESPACE}" &> /dev/null; then
            # Verify VPA has correct labels (proves operator created it, not something else)
            local managed_by
            managed_by=$(kubectl get vpa test-app-vpa -n "${TEST_NAMESPACE}" -o jsonpath='{.metadata.labels.app\.kubernetes\.io/managed-by}' 2>/dev/null)
            if [[ "${managed_by}" == "vpa-operator" ]]; then
                log_success "VPA 'test-app-vpa' created successfully with correct labels"
                TESTS_PASSED=$((TESTS_PASSED + 1))
                return 0
            else
                log_error "VPA exists but missing 'app.kubernetes.io/managed-by=vpa-operator' label"
                TESTS_FAILED=$((TESTS_FAILED + 1))
                return 1
            fi
        fi
        sleep 2
        retries=$((retries + 1))
    done

    log_error "VPA was not created within timeout"
    TESTS_FAILED=$((TESTS_FAILED + 1))
    
    # Debug info
    log_info "Debug: Operator logs:"
    kubectl logs -n "${OPERATOR_NAMESPACE}" -l control-plane=controller-manager --tail=50 || true
    return 1
}

#######################################
# Test: VPA configuration
#######################################
test_vpa_configuration() {
    log_step "Test: VPA configuration matches VpaManager spec"

    local vpa_update_mode
    vpa_update_mode=$(kubectl get vpa test-app-vpa -n "${TEST_NAMESPACE}" -o jsonpath='{.spec.updatePolicy.updateMode}' 2>/dev/null)

    if [[ "${vpa_update_mode}" == "Off" ]]; then
        log_success "VPA updateMode is correctly set to 'Off'"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        log_error "VPA updateMode is '${vpa_update_mode}', expected 'Off'"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Check target ref
    local target_name
    target_name=$(kubectl get vpa test-app-vpa -n "${TEST_NAMESPACE}" -o jsonpath='{.spec.targetRef.name}' 2>/dev/null)

    if [[ "${target_name}" == "test-app" ]]; then
        log_success "VPA targetRef correctly points to 'test-app' deployment"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        log_error "VPA targetRef is '${target_name}', expected 'test-app'"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

#######################################
# Test: VpaManager status
#######################################
test_vpamanager_status() {
    log_step "Test: VpaManager status is updated"

    # Wait for status to be updated with retries
    local retries=0
    local max_retries=15
    local managed_vpas=""
    
    while [[ $retries -lt $max_retries ]]; do
        managed_vpas=$(kubectl get vpamanager test-vpamanager -o jsonpath='{.status.managedVPAs}' 2>/dev/null)
        if [[ -n "${managed_vpas}" ]] && [[ "${managed_vpas}" -ge 1 ]]; then
            log_success "VpaManager status shows ${managed_vpas} managed VPA(s)"
            TESTS_PASSED=$((TESTS_PASSED + 1))
            return 0
        fi
        sleep 2
        retries=$((retries + 1))
    done

    # Also verify managedDeployments array is populated (catches schema mismatches)
    local managed_dep_name
    managed_dep_name=$(kubectl get vpamanager test-vpamanager -o jsonpath='{.status.managedDeployments[0].name}' 2>/dev/null)
    if [[ -n "${managed_dep_name}" ]]; then
        log_success "VpaManager status includes deployment reference: ${managed_dep_name}"
    else
        log_error "VpaManager status.managedDeployments not populated (possible schema mismatch)"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi

    log_error "VpaManager status shows '${managed_vpas}' managed VPAs, expected >= 1"
    TESTS_FAILED=$((TESTS_FAILED + 1))
}

#######################################
# Test: No errors in operator logs
#######################################
test_operator_no_errors() {
    log_step "Test: No reconciliation errors in operator logs"

    local pod_name
    pod_name=$(kubectl get pods -n "${OPERATOR_NAMESPACE}" -l control-plane=controller-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

    if [[ -z "${pod_name}" ]]; then
        log_error "Could not find operator pod"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi

    local error_lines
    error_lines=$(kubectl logs -n "${OPERATOR_NAMESPACE}" "${pod_name}" --tail=200 2>/dev/null | grep "ERROR" || true)
    
    if [[ -z "${error_lines}" ]]; then
        log_success "No errors found in operator logs"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        local error_count
        error_count=$(echo "${error_lines}" | wc -l | tr -d ' ')
        log_error "Found ${error_count} ERROR(s) in operator logs - possible bug or misconfiguration"
        log_info "Recent errors:"
        echo "${error_lines}" | tail -5
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

#######################################
# Test: VPA cleanup on deployment deletion
#######################################
test_vpa_cleanup() {
    log_step "Test: VPA cleanup when deployment is deleted"

    # Delete the deployment
    log_info "Deleting test deployment..."
    kubectl delete deployment test-app -n "${TEST_NAMESPACE}"

    # Wait for VPA to be cleaned up (either by operator or owner reference GC)
    log_info "Waiting for VPA cleanup..."
    local retries=0
    local max_retries=30
    while [[ $retries -lt $max_retries ]]; do
        if ! kubectl get vpa test-app-vpa -n "${TEST_NAMESPACE}" &> /dev/null; then
            log_success "VPA was cleaned up after deployment deletion"
            TESTS_PASSED=$((TESTS_PASSED + 1))
            return 0
        fi
        sleep 2
        retries=$((retries + 1))
    done

    log_error "VPA was not cleaned up within timeout"
    TESTS_FAILED=$((TESTS_FAILED + 1))
    return 1
}

#######################################
# Test: Disabled VpaManager
#######################################
test_disabled_vpamanager() {
    log_step "Test: Disabled VpaManager does not create VPAs"

    # Disable VpaManager FIRST
    log_info "Disabling VpaManager..."
    kubectl patch vpamanager test-vpamanager --type=merge -p '{"spec":{"enabled":false}}'
    
    # Wait for reconciliation to process the disable
    sleep 5

    # Now create test deployment
    log_info "Creating test deployment (with VpaManager disabled)..."
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app-disabled
  namespace: ${TEST_NAMESPACE}
  labels:
    app: test-app-disabled
    vpa-enabled: "true"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test-app-disabled
  template:
    metadata:
      labels:
        app: test-app-disabled
    spec:
      containers:
      - name: nginx
        image: nginx:alpine
        resources:
          requests:
            cpu: 10m
            memory: 32Mi
EOF

    # Wait and verify no VPA is created
    sleep 10
    if ! kubectl get vpa test-app-disabled-vpa -n "${TEST_NAMESPACE}" &> /dev/null; then
        log_success "No VPA created when VpaManager is disabled"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        log_error "VPA was created despite VpaManager being disabled"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

#######################################
# Cleanup
#######################################
cleanup() {
    if [[ "${SKIP_CLEANUP}" == "true" ]]; then
        log_warn "Skipping cleanup (--skip-cleanup specified)"
        log_info "To clean up manually, run:"
        log_info "  helm uninstall ${HELM_RELEASE_NAME} -n ${HELM_NAMESPACE}"
        log_info "  kubectl delete namespace ${TEST_NAMESPACE}"
        log_info "  kubectl delete vpamanager test-vpamanager"
        return 0
    fi

    log_step "Cleaning up test resources"

    # Delete VpaManager first
    kubectl delete vpamanager test-vpamanager --ignore-not-found=true 2>/dev/null || true

    # Delete test namespace
    kubectl delete namespace "${TEST_NAMESPACE}" --ignore-not-found=true 2>/dev/null || true

    # Uninstall Helm release
    helm uninstall "${HELM_RELEASE_NAME}" -n "${HELM_NAMESPACE}" --ignore-not-found 2>/dev/null || true
    kubectl delete namespace "${HELM_NAMESPACE}" --ignore-not-found=true 2>/dev/null || true

    log_success "Cleanup complete"
}

#######################################
# Print summary
#######################################
print_summary() {
    log_step "Test Summary"
    echo ""
    echo -e "  ${GREEN}Passed:${NC} ${TESTS_PASSED}"
    echo -e "  ${RED}Failed:${NC} ${TESTS_FAILED}"
    echo ""

    if [[ ${TESTS_FAILED} -gt 0 ]]; then
        log_error "Some tests failed!"
        return 1
    else
        log_success "All tests passed!"
        return 0
    fi
}

#######################################
# Main
#######################################
main() {
    parse_args "$@"

    echo ""
    echo "=========================================="
    echo "  VPA Operator Validation Script"
    echo "=========================================="
    echo ""

    # Set up trap for cleanup on exit
    trap cleanup EXIT

    check_prerequisites
    
    # Install VPA CRD (external dependency not included in Helm chart)
    log_step "Installing VPA CRD from upstream"
    kubectl apply -f https://raw.githubusercontent.com/kubernetes/autoscaler/master/vertical-pod-autoscaler/deploy/vpa-v1-crd-gen.yaml
    kubectl wait --for=condition=Established crd/verticalpodautoscalers.autoscaling.k8s.io --timeout="${TIMEOUT}s" || true
    
    deploy_operator
    
    create_test_resources

    # Run tests
    test_vpa_creation
    test_vpa_configuration
    test_vpamanager_status
    test_operator_no_errors
    test_vpa_cleanup
    test_disabled_vpamanager

    # Print results (cleanup happens via trap)
    print_summary
}

main "$@"
