#!/bin/bash

# Manually trigger memory consolidation on Raspberry Pi

set -e

NAMESPACE="${NAMESPACE:-agi}"
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_status() {
    echo -e "${BLUE}[CONSOLIDATION]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

echo "=========================================="
echo "Manual Memory Consolidation Trigger"
echo "=========================================="
echo ""

# Get HDN pod
HDN_POD=$(kubectl get pods -n $NAMESPACE -l app=hdn-server-rpi58 -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -z "$HDN_POD" ]; then
    print_error "HDN pod not found"
    print_status "Trying alternative label..."
    HDN_POD=$(kubectl get pods -n $NAMESPACE | grep hdn | head -1 | awk '{print $1}' || echo "")
    if [ -z "$HDN_POD" ]; then
        print_error "Could not find HDN pod"
        exit 1
    fi
fi

print_status "Triggering consolidation via HDN API..."
print_status "Pod: $HDN_POD"

# Check if endpoint exists (if 404, server needs rebuild)
print_status "Checking if endpoint is available..."

# Trigger consolidation via API (BusyBox wget uses --post-data, not --method)
RESPONSE=$(kubectl exec -n $NAMESPACE "$HDN_POD" -- wget -q -O- \
    --post-data='' \
    --header='Content-Type: application/json' \
    http://localhost:8080/api/v1/memory/consolidate 2>&1 || echo "")

if [ -z "$RESPONSE" ]; then
    print_error "No response from HDN"
    exit 1
fi

# Check if response indicates success
if echo "$RESPONSE" | grep -q "success"; then
    print_success "Consolidation triggered successfully"
    echo "$RESPONSE" | python3 -c '
import sys, json
try:
    data = json.load(sys.stdin)
    print(f"  Success: {data.get(\"success\", False)}")
    print(f"  Message: {data.get(\"message\", \"\")}")
    print(f"  Timestamp: {data.get(\"timestamp\", \"\")}")
except:
    print("  Response received (could not parse JSON)")
    print(f"  Raw: {sys.stdin.read()}")
' 2>/dev/null || echo "  Response: $RESPONSE"
    
    print_status "Consolidation is running in background"
    print_status "Check logs with: kubectl logs -n $NAMESPACE $HDN_POD | grep CONSOLIDATION"
else
    print_error "Unexpected response from HDN"
    echo "  Response: $RESPONSE"
    exit 1
fi

echo ""
print_success "Done!"

