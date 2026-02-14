#!/bin/bash

# Fix token whitespace issues in Kubernetes secret
# This is often the cause of "token signature invalid" errors

set -e

NAMESPACE="agi"
SECURE_DIR="${1:-/home/stevef/dev/agi/secure}"

echo "=========================================="
echo "Fix Token Whitespace Issues"
echo "=========================================="
echo ""

# Check if token.txt exists
if [ ! -f "$SECURE_DIR/token.txt" ]; then
    echo "❌ token.txt not found at $SECURE_DIR/token.txt"
    echo "   Generate it first with: ./generate-vendor-token.sh $SECURE_DIR"
    exit 1
fi

echo "1. Reading and cleaning local token..."
# Read token and remove ALL whitespace (newlines, spaces, tabs, etc.)
CLEAN_TOKEN=$(cat "$SECURE_DIR/token.txt" | tr -d '\n\r\t ')

if [ -z "$CLEAN_TOKEN" ]; then
    echo "❌ Token is empty after cleaning"
    exit 1
fi

echo "   ✅ Token cleaned (length: ${#CLEAN_TOKEN} chars)"
echo ""

# Save cleaned token back to file (without trailing newline)
echo "2. Saving cleaned token to file..."
printf '%s' "$CLEAN_TOKEN" > "$SECURE_DIR/token.txt"
echo "   ✅ Saved to $SECURE_DIR/token.txt"
echo ""

# Update Kubernetes secret
echo "3. Updating Kubernetes secret with cleaned token..."
kubectl create secret generic secure-vendor -n $NAMESPACE \
    --from-file=token="$SECURE_DIR/token.txt" \
    --dry-run=client -o yaml | kubectl apply -f -

echo "   ✅ Kubernetes secret updated"
echo ""

# Verify the update
echo "4. Verifying updated secret..."
K8S_TOKEN=$(kubectl get secret secure-vendor -n $NAMESPACE -o jsonpath='{.data.token}' | base64 -d)

# Remove whitespace for comparison
K8S_CLEAN=$(echo "$K8S_TOKEN" | tr -d '\n\r\t ')

if [ "$CLEAN_TOKEN" = "$K8S_CLEAN" ]; then
    echo "   ✅ Secret updated correctly - tokens match"
else
    echo "   ⚠️  Warning: Tokens still differ after update"
    echo "   Local: ${#CLEAN_TOKEN} chars"
    echo "   K8s:   ${#K8S_CLEAN} chars"
fi

echo ""
echo "=========================================="
echo "Next Steps"
echo "=========================================="
echo ""
echo "1. Wait for next cronjob run (wiki-bootstrapper runs every 10 minutes)"
echo "   Or manually trigger: kubectl delete job -n agi -l job-name=wiki-bootstrapper-cronjob"
echo ""
echo "2. Check pod logs:"
echo "   kubectl logs -n agi -l app=wiki-bootstrapper --tail=50"
echo ""
echo "3. If still failing, check token format:"
echo "   ./check-token-format.sh $SECURE_DIR"
echo ""










