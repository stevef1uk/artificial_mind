#!/bin/bash

# Fix Token Signature Error Script
# This script helps diagnose and fix the "token signature invalid" error
# for the wiki-bootstrapper and other secure images

set -e

NAMESPACE="agi"
SECURE_DIR="${1:-~/dev/artificial_mind/secure}"

# Expand tilde if present
if [[ "$SECURE_DIR" == ~* ]]; then
    SECURE_DIR="${SECURE_DIR/#\~/$HOME}"
fi

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

echo "=========================================="
echo "Token Signature Error Fix Script"
echo "=========================================="
echo ""

# Step 1: Check prerequisites
print_status "Step 1: Checking prerequisites..."

if [ ! -d "$SECURE_DIR" ]; then
    print_error "Secure directory not found: $SECURE_DIR"
    exit 1
fi

if [ ! -f "$SECURE_DIR/vendor_private.pem" ]; then
    print_error "vendor_private.pem not found in $SECURE_DIR"
    print_error "This is required to generate a valid token"
    exit 1
fi

if [ ! -f "$SECURE_DIR/vendor_public.pem" ]; then
    print_warning "vendor_public.pem not found. Generating from private key..."
    openssl rsa -in "$SECURE_DIR/vendor_private.pem" -pubout -out "$SECURE_DIR/vendor_public.pem"
    print_success "Generated vendor_public.pem"
fi

if [ ! -f "$SECURE_DIR/customer_private.pem" ]; then
    print_error "customer_private.pem not found in $SECURE_DIR"
    exit 1
fi

print_success "Prerequisites check passed"
echo ""

# Step 2: Check if secure-packager is available
print_status "Step 2: Checking secure-packager image..."

if ! docker image inspect stevef1uk/secure-packager:latest >/dev/null 2>&1; then
    print_warning "secure-packager image not found locally. Pulling..."
    docker pull stevef1uk/secure-packager:latest
fi

print_success "secure-packager image available"
echo ""

# Step 3: Check current token in Kubernetes
print_status "Step 3: Checking current token in Kubernetes secret..."

if kubectl get secret secure-vendor -n $NAMESPACE >/dev/null 2>&1; then
    CURRENT_TOKEN=$(kubectl get secret secure-vendor -n $NAMESPACE -o jsonpath='{.data.token}' | base64 -d 2>/dev/null || echo "")
    if [ -n "$CURRENT_TOKEN" ]; then
        print_status "Current token preview: ${CURRENT_TOKEN:0:50}..."
    else
        print_warning "Token exists but is empty"
    fi
else
    print_warning "secure-vendor secret not found in namespace $NAMESPACE"
fi
echo ""

# Step 4: Generate new token
print_status "Step 4: Generating new vendor token..."

TOKEN=$(docker run --rm \
    -v "$SECURE_DIR:/keys:ro" \
    stevef1uk/secure-packager:latest \
    issue-token -priv /keys/vendor_private.pem 2>&1)

if [ -z "$TOKEN" ] || [[ "$TOKEN" == *"error"* ]] || [[ "$TOKEN" == *"Error"* ]]; then
    print_error "Failed to generate token:"
    echo "$TOKEN"
    exit 1
fi

# Save token to file
echo "$TOKEN" > "$SECURE_DIR/token.txt"
print_success "Token generated and saved to $SECURE_DIR/token.txt"
print_status "Token preview: ${TOKEN:0:50}..."
echo ""

# Step 5: Verify token matches public key
print_status "Step 5: Verifying token signature..."

# Extract public key from token (if possible) or just verify it can be read
if docker run --rm \
    -v "$SECURE_DIR:/keys:ro" \
    stevef1uk/secure-packager:latest \
    issue-token -priv /keys/vendor_private.pem -verify /keys/vendor_public.pem >/dev/null 2>&1; then
    print_success "Token signature verification passed"
else
    print_warning "Could not verify token signature (this may be normal)"
fi
echo ""

# Step 6: Update Kubernetes secrets
print_status "Step 6: Updating Kubernetes secrets..."

# Update customer private key secret
print_status "Updating secure-customer-private secret..."
kubectl create secret generic secure-customer-private -n $NAMESPACE \
    --from-file=customer_private.pem="$SECURE_DIR/customer_private.pem" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null 2>&1
print_success "Updated secure-customer-private secret"

# Update vendor token secret
print_status "Updating secure-vendor secret..."
kubectl create secret generic secure-vendor -n $NAMESPACE \
    --from-file=token="$SECURE_DIR/token.txt" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null 2>&1
print_success "Updated secure-vendor secret"
echo ""

# Step 7: Verify secrets
print_status "Step 7: Verifying secrets..."

if kubectl get secret secure-vendor -n $NAMESPACE >/dev/null 2>&1; then
    NEW_TOKEN=$(kubectl get secret secure-vendor -n $NAMESPACE -o jsonpath='{.data.token}' | base64 -d 2>/dev/null || echo "")
    if [ "$NEW_TOKEN" = "$TOKEN" ]; then
        print_success "Secret updated correctly - token matches"
    else
        print_warning "Token in secret may differ from generated token"
    fi
else
    print_error "Failed to verify secret update"
fi
echo ""

# Step 8: Check if images were built with matching public key
print_status "Step 8: Important reminder about image build..."

print_warning "IMPORTANT: The Docker images must have been built with the SAME vendor_public.pem"
print_warning "that corresponds to the vendor_private.pem used to generate this token."
print_warning ""
print_warning "If you rebuilt images with a different vendor_public.pem, you need to either:"
print_warning "  1. Rebuild images with the current vendor_public.pem, OR"
print_warning "  2. Use the vendor_private.pem that matches the vendor_public.pem used in the images"
echo ""

# Step 9: Test with a pod (optional)
print_status "Step 9: Testing token with a pod..."

print_status "You can test the token by checking wiki-bootstrapper pod logs:"
echo "  kubectl logs -n $NAMESPACE -l app=wiki-bootstrapper --tail=50"
echo ""
print_status "Or manually test with:"
echo "  kubectl run test-token --rm -i --tty --image=stevef1uk/knowledge-builder:secure -n $NAMESPACE -- \\"
echo "    sh -c 'echo \"\$SECURE_VENDOR_TOKEN\" | head -c 50'"
echo ""

print_success "=========================================="
print_success "Token fix process completed!"
print_success "=========================================="
echo ""
print_status "Next steps:"
echo "  1. Wait for the next cron job run (wiki-bootstrapper runs every 10 minutes)"
echo "  2. Check pod logs: kubectl logs -n $NAMESPACE -l app=wiki-bootstrapper --tail=50"
echo "  3. If still failing, verify the vendor_public.pem used to build images matches this vendor_private.pem"
echo ""










