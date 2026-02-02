#!/bin/bash

# Check token format in Kubernetes secret for corruption/whitespace issues

set -e

NAMESPACE="agi"

echo "=========================================="
echo "Token Format Check"
echo "=========================================="
echo ""

# Get token from Kubernetes
echo "1. Reading token from Kubernetes secret..."
if ! kubectl get secret secure-vendor -n $NAMESPACE >/dev/null 2>&1; then
    echo "❌ secure-vendor secret not found"
    exit 1
fi

K8S_TOKEN=$(kubectl get secret secure-vendor -n $NAMESPACE -o jsonpath='{.data.token}' | base64 -d)

if [ -z "$K8S_TOKEN" ]; then
    echo "❌ Token is empty in Kubernetes secret"
    exit 1
fi

echo "✅ Token found in Kubernetes"
echo ""

# Check token characteristics
echo "2. Analyzing token format..."
TOKEN_LENGTH=${#K8S_TOKEN}
echo "   Length: $TOKEN_LENGTH characters"

# Check for newlines
if echo "$K8S_TOKEN" | grep -q $'\n'; then
    echo "   ⚠️  WARNING: Token contains newlines!"
    echo "   This will cause signature verification to fail!"
    NEWLINE_COUNT=$(echo "$K8S_TOKEN" | grep -c $'\n' || echo "0")
    echo "   Found $NEWLINE_COUNT newline(s)"
else
    echo "   ✅ No newlines found"
fi

# Check for carriage returns
if echo "$K8S_TOKEN" | grep -q $'\r'; then
    echo "   ⚠️  WARNING: Token contains carriage returns!"
    echo "   This will cause signature verification to fail!"
else
    echo "   ✅ No carriage returns found"
fi

# Check for leading/trailing whitespace
TRIMMED=$(echo "$K8S_TOKEN" | tr -d '\n\r' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
if [ "$K8S_TOKEN" != "$TRIMMED" ]; then
    echo "   ⚠️  WARNING: Token has leading/trailing whitespace!"
    echo "   Original length: $TOKEN_LENGTH"
    echo "   Trimmed length: ${#TRIMMED}"
else
    echo "   ✅ No leading/trailing whitespace"
fi

# Check if it's valid base64-like
if [[ "$K8S_TOKEN" =~ ^[A-Za-z0-9+/=_-]+$ ]]; then
    echo "   ✅ Token format looks valid (base64-like)"
else
    echo "   ⚠️  Token contains unexpected characters"
    INVALID_CHARS=$(echo "$K8S_TOKEN" | tr -d 'A-Za-z0-9+/=_-' | head -c 20)
    echo "   Invalid characters found: '$INVALID_CHARS'"
fi

echo ""

# Show token preview
echo "3. Token preview:"
echo "   First 60 chars: ${K8S_TOKEN:0:60}..."
echo "   Last 60 chars:  ...${K8S_TOKEN: -60}"
echo ""

# Compare with local token if it exists
SECURE_DIR="${1:-/home/stevef/dev/agi/secure}"
if [ -f "$SECURE_DIR/token.txt" ]; then
    echo "4. Comparing with local token.txt..."
    LOCAL_TOKEN=$(cat "$SECURE_DIR/token.txt" | tr -d '\n\r')
    LOCAL_LENGTH=${#LOCAL_TOKEN}
    
    if [ "$K8S_TOKEN" = "$LOCAL_TOKEN" ]; then
        echo "   ✅ Tokens match exactly"
    else
        echo "   ⚠️  Tokens differ!"
        echo "   Kubernetes length: $TOKEN_LENGTH"
        echo "   Local length: $LOCAL_LENGTH"
        
        # Check if it's just whitespace difference
        K8S_TRIMMED=$(echo "$K8S_TOKEN" | tr -d '\n\r ')
        LOCAL_TRIMMED=$(echo "$LOCAL_TOKEN" | tr -d '\n\r ')
        if [ "$K8S_TRIMMED" = "$LOCAL_TRIMMED" ]; then
            echo "   💡 Tokens match when whitespace is removed"
            echo "   💡 The issue is likely whitespace in the Kubernetes secret!"
        fi
    fi
else
    echo "4. No local token.txt found to compare"
fi

echo ""
echo "=========================================="
echo "Recommendations"
echo "=========================================="
echo ""

if echo "$K8S_TOKEN" | grep -q $'\n\|\r'; then
    echo "🔴 CRITICAL: Token has newlines/carriage returns!"
    echo ""
    echo "Fix by updating the secret with a clean token:"
    echo "  1. Ensure token.txt has no trailing newline:"
    echo "     printf '%s' \"\$(cat $SECURE_DIR/token.txt | tr -d '\n\r')\" > $SECURE_DIR/token.txt"
    echo ""
    echo "  2. Update Kubernetes secret:"
    echo "     cd ~/dev/artificial_mind/k3s"
    echo "     ./update-secrets.sh $SECURE_DIR"
    echo ""
elif [ "$K8S_TOKEN" != "$TRIMMED" ]; then
    echo "🟡 WARNING: Token has whitespace issues!"
    echo ""
    echo "Fix by updating the secret with a trimmed token"
else
    echo "✅ Token format looks correct"
    echo ""
    echo "If you're still getting signature errors, the issue might be:"
    echo "  1. Token was signed with different vendor_private.pem than used to build image"
    echo "  2. Image was rebuilt with different vendor_public.pem"
    echo "  3. Token has expired (check token expiration date)"
fi







