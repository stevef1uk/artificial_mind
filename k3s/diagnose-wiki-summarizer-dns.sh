#!/bin/bash

# Diagnose DNS issues for wiki-summarizer

set -e

NAMESPACE="agi"
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_status() {
    echo -e "${BLUE}[CHECK]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[FAIL]${NC} $1"
}

echo "=========================================="
echo "Wiki-Summarizer DNS Diagnostic"
echo "=========================================="
echo ""

# Find a wiki-summarizer pod
POD_NAME=$(kubectl get pods -n $NAMESPACE -l app=wiki-summarizer -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -z "$POD_NAME" ]; then
    print_error "No wiki-summarizer pod found"
    exit 1
fi

print_status "Using pod: $POD_NAME"
echo ""

# Step 1: Check DNS resolution
print_status "Step 1: Testing DNS resolution..."
echo "  Testing: weaviate.agi.svc.cluster.local"
DNS_RESULT=$(kubectl exec -n $NAMESPACE "$POD_NAME" -- nslookup weaviate.agi.svc.cluster.local 2>&1 || echo "FAILED")
if echo "$DNS_RESULT" | grep -q "Name:"; then
    print_success "DNS resolution works"
    echo "$DNS_RESULT" | grep -E "Name:|Address:" | sed 's/^/    /'
else
    print_error "DNS resolution failed"
    echo "$DNS_RESULT" | sed 's/^/    /'
fi

# Step 2: Check CoreDNS
print_status "Step 2: Checking CoreDNS..."
COREDNS_POD=$(kubectl get pods -n kube-system -l k8s-app=kube-dns -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$COREDNS_POD" ]; then
    print_success "CoreDNS pod found: $COREDNS_POD"
    COREDNS_STATUS=$(kubectl get pod -n kube-system "$COREDNS_POD" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
    if [ "$COREDNS_STATUS" = "Running" ]; then
        print_success "CoreDNS is running"
    else
        print_error "CoreDNS status: $COREDNS_STATUS"
    fi
else
    print_error "CoreDNS pod not found"
fi

# Step 3: Check pod DNS config
print_status "Step 3: Checking pod DNS configuration..."
DNS_POLICY=$(kubectl get pod -n $NAMESPACE "$POD_NAME" -o jsonpath='{.spec.dnsPolicy}' 2>/dev/null || echo "Unknown")
print_status "DNS Policy: $DNS_POLICY"

DNS_CONFIG=$(kubectl get pod -n $NAMESPACE "$POD_NAME" -o jsonpath='{.spec.dnsConfig}' 2>/dev/null || echo "")
if [ -n "$DNS_CONFIG" ] && [ "$DNS_CONFIG" != "null" ]; then
    print_status "DNS Config: $DNS_CONFIG"
else
    print_warning "No custom DNS config"
fi

# Step 4: Test direct IP connection
print_status "Step 4: Testing direct connection to Weaviate service IP..."
WEAVIATE_IP=$(kubectl get svc -n $NAMESPACE weaviate -o jsonpath='{.spec.clusterIP}' 2>/dev/null || echo "")
if [ -n "$WEAVIATE_IP" ]; then
    print_status "Weaviate ClusterIP: $WEAVIATE_IP"
    CONNECT_TEST=$(kubectl exec -n $NAMESPACE "$POD_NAME" -- wget -q -O- --timeout=5 "http://$WEAVIATE_IP:8080/v1/.well-known/ready" 2>&1 || echo "FAILED")
    if [ "$CONNECT_TEST" = "ok" ]; then
        print_success "Direct IP connection works"
    else
        print_error "Direct IP connection failed: $CONNECT_TEST"
    fi
else
    print_error "Could not get Weaviate service IP"
fi

# Step 5: Check /etc/resolv.conf in pod
print_status "Step 5: Checking /etc/resolv.conf in pod..."
RESOLV_CONF=$(kubectl exec -n $NAMESPACE "$POD_NAME" -- cat /etc/resolv.conf 2>&1 || echo "FAILED")
if [ "$RESOLV_CONF" != "FAILED" ]; then
    print_success "resolv.conf contents:"
    echo "$RESOLV_CONF" | sed 's/^/    /'
else
    print_error "Could not read resolv.conf"
fi

# Step 6: Test alternative service name
print_status "Step 6: Testing alternative service name resolution..."
ALT_TEST=$(kubectl exec -n $NAMESPACE "$POD_NAME" -- nslookup weaviate 2>&1 || echo "FAILED")
if echo "$ALT_TEST" | grep -q "Name:"; then
    print_success "Short service name works"
else
    print_warning "Short service name failed"
fi

echo ""
echo "=========================================="
echo "Diagnostic Complete"
echo "=========================================="
echo ""
echo "If DNS is failing, try:"
echo "  1. Check CoreDNS logs: kubectl logs -n kube-system -l k8s-app=kube-dns"
echo "  2. Restart CoreDNS: kubectl rollout restart deployment/coredns -n kube-system"
echo "  3. Use direct IP or short service name in configuration"
echo ""

