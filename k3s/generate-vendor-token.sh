#!/bin/bash

# Generate a proper vendor token using the secure-packager
# This must be run on a machine with Docker and access to the vendor_private.pem

set -e

SECURE_DIR="${1:-~/dev/artificial_mind/secure}"

# Expand tilde if present
if [[ "$SECURE_DIR" == ~* ]]; then
    SECURE_DIR="${SECURE_DIR/#\~/$HOME}"
fi

echo "Generating vendor token..."
echo "Using secure directory: $SECURE_DIR"

# Check if vendor private key exists
if [ ! -f "$SECURE_DIR/vendor_private.pem" ]; then
    echo "Error: vendor_private.pem not found in $SECURE_DIR"
    echo "Please generate it first:"
    echo "  openssl genrsa -out $SECURE_DIR/vendor_private.pem 2048"
    exit 1
fi

# Check if secure-packager image is available
if ! docker image inspect stevef1uk/secure-packager:latest >/dev/null 2>&1; then
    echo "Pulling secure-packager image..."
    docker pull stevef1uk/secure-packager:latest
fi

# Generate the token
echo "Generating vendor token using issue-token..."
TOKEN=$(docker run --rm \
    -v "$SECURE_DIR:/keys" \
    stevef1uk/secure-packager:latest \
    issue-token -priv /keys/vendor_private.pem -expiry 2026-12-31 -company SJFisher -email stevef@gmail.com -out /keys/token.txt)

if [ -z "$TOKEN" ]; then
    echo "Error: Failed to generate token"
    exit 1
fi

# Save token to file
echo "$TOKEN" 
echo "✅ Vendor token generated and saved to $SECURE_DIR/token.txt"
echo ""
echo "Token preview (first 50 chars): ${TOKEN:0:50}..."
echo ""
echo "Now update the Kubernetes secret:"
echo "  cd ~/dev/artificial_mind/k3s"
echo "  ./update-secrets.sh $SECURE_DIR"








