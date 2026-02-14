#!/bin/bash

# Update Kubernetes secrets with new secure keys
# Run this on your Raspberry Pi after generating new keys

set -e

NAMESPACE="agi"
SECURE_DIR="${1:-~/dev/artificial_mind/secure}"

# Expand tilde if present
if [[ "$SECURE_DIR" == ~* ]]; then
    SECURE_DIR="${SECURE_DIR/#\~/$HOME}"
fi

echo "Updating secrets in namespace: $NAMESPACE"
echo "Using secure directory: $SECURE_DIR"

# Check if secure directory exists
if [ ! -d "$SECURE_DIR" ]; then
    echo "Error: Secure directory not found: $SECURE_DIR"
    exit 1
fi

# Check if customer private key exists
if [ ! -f "$SECURE_DIR/customer_private.pem" ]; then
    echo "Error: customer_private.pem not found in $SECURE_DIR"
    exit 1
fi

# Update secure-customer-private secret
echo "Updating secure-customer-private secret..."
kubectl create secret generic secure-customer-private -n $NAMESPACE \
    --from-file=customer_private.pem="$SECURE_DIR/customer_private.pem" \
    --dry-run=client -o yaml | kubectl apply -f -

# Check if token.txt exists, otherwise create a placeholder
if [ ! -f "$SECURE_DIR/token.txt" ]; then
    echo "Warning: token.txt not found. Creating placeholder token..."
    echo "You may need to generate a proper token using issue-token tool"
    echo "placeholder-token-$(date +%s)" > "$SECURE_DIR/token.txt"
fi

# Update secure-vendor secret
echo "Updating secure-vendor secret..."
kubectl create secret generic secure-vendor -n $NAMESPACE \
    --from-file=token="$SECURE_DIR/token.txt" \
    --dry-run=client -o yaml | kubectl apply -f -

echo ""
echo "✅ Secrets updated successfully!"
echo ""
echo "Updated secrets:"
kubectl get secrets -n $NAMESPACE | grep -E "secure|customer|vendor"
echo ""
echo "Note: If you need to generate a proper vendor token, use:"
echo "  docker run --rm -v $SECURE_DIR:/keys stevef1uk/secure-packager:latest \\"
echo "    issue-token -priv /keys/vendor_private.pem"









