#!/usr/bin/env bash
# e2e-kind.sh - End-to-end test for kubectl-hpa-status using kind
#
# Prerequisites:
#   - kind (https://kind.sigs.k8s.io/)
#   - kubectl
#   - go
#
# Usage:
#   ./scripts/e2e-kind.sh
#
# This script:
#   1. Creates a kind cluster named hpa-status-e2e
#   2. Installs metrics-server
#   3. Deploys sample HPAs
#   4. Runs the plugin binary against the cluster
#   5. Validates output
#   6. Tears down the cluster

set -euo pipefail

CLUSTER_NAME="hpa-status-e2e"
KIND_NODE_IMAGE="${KIND_NODE_IMAGE:-kindest/node:v1.31.0}"
INSTALL_KEDA="${INSTALL_KEDA:-false}"
INSTALL_VPA="${INSTALL_VPA:-false}"
METRICS_SERVER_VERSION="${METRICS_SERVER_VERSION:-v0.7.0}"
KEDA_VERSION="${KEDA_VERSION:-v2.20.1}"
VPA_VERSION="${VPA_VERSION:-vertical-pod-autoscaler-1.3.1}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
MANIFEST_DIR="$PROJECT_DIR/testdata/manifests"
BINARY="$PROJECT_DIR/kubectl-hpa-status"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[e2e]${NC} $*"; }
warn() { echo -e "${YELLOW}[e2e]${NC} $*"; }
fail() { echo -e "${RED}[e2e FAIL]${NC} $*" >&2; exit 1; }

cleanup() {
    if kind get clusters 2>/dev/null | grep -q "$CLUSTER_NAME"; then
        log "Deleting kind cluster $CLUSTER_NAME..."
        kind delete cluster --name "$CLUSTER_NAME"
    fi
}
trap cleanup EXIT

# --- Build ---
log "Building kubectl-hpa-status..."
(cd "$PROJECT_DIR" && go build -o "$BINARY" .) || fail "Build failed"

# --- Create cluster ---
if kind get clusters 2>/dev/null | grep -q "$CLUSTER_NAME"; then
    warn "Cluster $CLUSTER_NAME already exists, deleting..."
    kind delete cluster --name "$CLUSTER_NAME"
fi
log "Creating kind cluster $CLUSTER_NAME with $KIND_NODE_IMAGE..."
kind create cluster --name "$CLUSTER_NAME" --image "$KIND_NODE_IMAGE" --wait 60s

export KUBECONFIG="$(kind get kubeconfig --name "$CLUSTER_NAME")"

# --- Install metrics-server ---
log "Installing metrics-server..."
kubectl apply -f "https://github.com/kubernetes-sigs/metrics-server/releases/download/${METRICS_SERVER_VERSION}/components.yaml"
kubectl patch deployment metrics-server -n kube-system --type='json' \
    -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'
log "Waiting for metrics-server to be ready..."
kubectl wait --for=condition=available deployment/metrics-server -n kube-system --timeout=120s

# --- Deploy test manifests ---
log "Applying test manifests..."
kubectl apply -f "$MANIFEST_DIR/deployment-web.yaml"
kubectl apply -f "$MANIFEST_DIR/deployment-web-multi.yaml"
kubectl apply -f "$MANIFEST_DIR/hpa-web.yaml"
kubectl apply -f "$MANIFEST_DIR/hpa-web-multi.yaml"
kubectl apply -f "$MANIFEST_DIR/hpa-broken.yaml"
log "Waiting for deployments to be ready..."
kubectl wait --for=condition=available deployment/web -n default --timeout=60s || true
kubectl wait --for=condition=available deployment/web-multi -n default --timeout=60s || true

# --- Wait for HPA to populate metrics ---
log "Waiting for HPA metrics to populate (30s)..."
sleep 30

# --- Optional: Install KEDA CRDs ---
if [ "$INSTALL_KEDA" = "true" ]; then
    log "Installing KEDA CRDs..."
    kubectl apply --server-side -f "https://github.com/kedacore/keda/releases/download/${KEDA_VERSION}/keda-${KEDA_VERSION#v}-crds.yaml" 2>/dev/null || warn "KEDA CRD install failed (non-fatal)"
fi

# --- Optional: Install VPA CRDs ---
if [ "$INSTALL_VPA" = "true" ]; then
    log "Installing VPA CRDs..."
    kubectl apply --server-side -f "https://github.com/kubernetes/autoscaler/releases/download/${VPA_VERSION}/vpa-crds.yaml" 2>/dev/null || warn "VPA CRD install failed (non-fatal)"
fi

# --- Test: list command ---
log "Testing: list command..."
OUTPUT=$("$BINARY" list -A 2>&1) || fail "list command failed: $OUTPUT"
echo "$OUTPUT" | grep -q "NAMESPACE" || fail "list output missing header"
echo "$OUTPUT" | grep -q "web" || fail "list output missing 'web' HPA"
log "  ✓ list command works"

# --- Test: status command ---
log "Testing: status command..."
OUTPUT=$("$BINARY" status web -n default 2>&1) || fail "status command failed: $OUTPUT"
echo "$OUTPUT" | grep -q "HPA default/web" || fail "status output missing HPA header"
log "  ✓ status command works"

# --- Test: explained status ---
log "Testing: status --explain..."
OUTPUT=$("$BINARY" status web -n default --explain 2>&1) || fail "status --explain failed: $OUTPUT"
echo "$OUTPUT" | grep -q "Interpretation" || fail "status --explain output missing Interpretation"
log "  ✓ status --explain works"

# --- Test: JSON output ---
log "Testing: JSON output..."
OUTPUT=$("$BINARY" status web -n default -o json 2>&1) || fail "JSON output failed: $OUTPUT"
echo "$OUTPUT" | python3 -m json.tool > /dev/null 2>&1 || fail "JSON output is not valid JSON"
log "  ✓ JSON output works"

# --- Test: wide list ---
log "Testing: wide list..."
OUTPUT=$("$BINARY" list -A --wide 2>&1) || fail "wide list failed: $OUTPUT"
echo "$OUTPUT" | grep -q "TARGET" || fail "wide output missing TARGET column"
echo "$OUTPUT" | grep -q "MIN" || fail "wide output missing MIN column"
log "  ✓ wide list works"

# --- Test: filter ---
log "Testing: filter..."
OUTPUT=$("$BINARY" list -A --filter all 2>&1) || fail "filter failed: $OUTPUT"
log "  ✓ filter works"

# --- Test: version ---
log "Testing: version..."
OUTPUT=$("$BINARY" --version 2>&1) || fail "version command failed: $OUTPUT"
log "  ✓ version command works"

# --- Test: scan (problem list) ---
log "Testing: scan command..."
OUTPUT=$("$BINARY" scan 2>&1) || true  # scan may return non-zero if problems found
log "  ✓ scan command works"

# --- Test: suggest ---
log "Testing: suggest on broken HPA..."
set +e
OUTPUT=$("$BINARY" status broken -n default --suggest 2>&1)
SUGGEST_RC=$?
set -e
if [ "$SUGGEST_RC" -gt 2 ]; then
    fail "suggest command failed unexpectedly (exit=${SUGGEST_RC}): $OUTPUT"
fi
echo "$OUTPUT" | grep -q "HPA default/broken" || fail "suggest output did not analyze the requested HPA: $OUTPUT"
log "  ✓ suggest command works"

log ""
log "============================================"
log "  All E2E tests passed!"
log "============================================"
