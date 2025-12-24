#!/bin/bash

# Debug script for token signature verification issues
# This helps identify mismatches between keys and tokens

set -e

SECURE_DIR="${1:-~/dev/artificial_mind/secure}"

# Expand tilde if present
if [[ "$SECURE_DIR" == ~* ]]; then
    SECURE_DIR="${SECURE_DIR/#\~/$HOME}"
fi

echo "=========================================="
echo "Token Signature Debug Script"
echo "=========================================="
echo ""

# Check if secure directory exists
if [ ! -d "$SECURE_DIR" ]; then
    echo "❌ Error: Secure directory not found: $SECURE_DIR"
    exit 1
fi

echo "📁 Using secure directory: $SECURE_DIR"
echo ""

# Check for required keys
echo "1. Checking for required keys..."
if [ ! -f "$SECURE_DIR/vendor_private.pem" ]; then
    echo "   ❌ vendor_private.pem not found"
    exit 1
else
    echo "   ✅ vendor_private.pem found"
fi

if [ ! -f "$SECURE_DIR/vendor_public.pem" ]; then
    echo "   ❌ vendor_public.pem not found"
    echo "   💡 Generating from vendor_private.pem..."
    openssl rsa -in "$SECURE_DIR/vendor_private.pem" -pubout -out "$SECURE_DIR/vendor_public.pem" 2>/dev/null || {
        echo "   ❌ Failed to generate vendor_public.pem"
        exit 1
    }
    echo "   ✅ vendor_public.pem generated"
else
    echo "   ✅ vendor_public.pem found"
fi

if [ ! -f "$SECURE_DIR/customer_private.pem" ]; then
    echo "   ❌ customer_private.pem not found"
    exit 1
else
    echo "   ✅ customer_private.pem found"
fi

echo ""

# Verify key pair matches
echo "2. Verifying vendor key pair matches..."
VENDOR_PRIV_HASH=$(openssl rsa -in "$SECURE_DIR/vendor_private.pem" -pubout 2>/dev/null | openssl md5 | cut -d' ' -f2)
VENDOR_PUB_HASH=$(openssl rsa -pubin -in "$SECURE_DIR/vendor_public.pem" -pubout 2>/dev/null | openssl md5 | cut -d' ' -f2)

if [ "$VENDOR_PRIV_HASH" = "$VENDOR_PUB_HASH" ]; then
    echo "   ✅ Vendor key pair matches"
else
    echo "   ❌ Vendor key pair MISMATCH!"
    echo "   💡 The vendor_private.pem and vendor_public.pem don't match"
    echo "   💡 Regenerate vendor_public.pem:"
    echo "      openssl rsa -in $SECURE_DIR/vendor_private.pem -pubout -out $SECURE_DIR/vendor_public.pem"
    exit 1
fi

echo ""

# Check current token
echo "3. Checking current token..."
if [ ! -f "$SECURE_DIR/token.txt" ]; then
    echo "   ⚠️  token.txt not found - will generate new one"
else
    TOKEN=$(cat "$SECURE_DIR/token.txt" | tr -d '\n\r ')
    if [ -z "$TOKEN" ]; then
        echo "   ⚠️  token.txt is empty - will generate new one"
    else
        echo "   ✅ token.txt found (length: ${#TOKEN} chars)"
        echo "   📋 Token preview: ${TOKEN:0:60}..."
        
        # Check if token looks valid (base64-like)
        if [[ "$TOKEN" =~ ^[A-Za-z0-9+/=_-]+$ ]]; then
            echo "   ✅ Token format looks valid"
        else
            echo "   ⚠️  Token format may be invalid (contains unexpected characters)"
        fi
    fi
fi

echo ""

# Check Kubernetes secret
echo "4. Checking Kubernetes secret..."
if kubectl get secret secure-vendor -n agi >/dev/null 2>&1; then
    echo "   ✅ secure-vendor secret exists"
    K8S_TOKEN=$(kubectl get secret secure-vendor -n agi -o jsonpath='{.data.token}' | base64 -d 2>/dev/null | tr -d '\n\r ')
    if [ -n "$K8S_TOKEN" ]; then
        echo "   ✅ Token in Kubernetes secret found (length: ${#K8S_TOKEN} chars)"
        
        # Compare with local token if it exists
        if [ -f "$SECURE_DIR/token.txt" ]; then
            LOCAL_TOKEN=$(cat "$SECURE_DIR/token.txt" | tr -d '\n\r ')
            if [ "$LOCAL_TOKEN" = "$K8S_TOKEN" ]; then
                echo "   ✅ Kubernetes token matches local token.txt"
            else
                echo "   ⚠️  Kubernetes token differs from local token.txt"
                echo "   💡 Consider updating Kubernetes secret with: ./update-secrets.sh $SECURE_DIR"
            fi
        fi
    else
        echo "   ❌ Token in Kubernetes secret is empty"
    fi
else
    echo "   ❌ secure-vendor secret not found in namespace 'agi'"
    echo "   💡 Create it with: ./update-secrets.sh $SECURE_DIR"
fi

echo ""

# Test token generation
echo "5. Testing token generation..."
if docker image inspect stevef1uk/secure-packager:latest >/dev/null 2>&1; then
    echo "   ✅ secure-packager image available"
    echo "   🔄 Generating test token..."
    
    TEST_TOKEN=$(docker run --rm \
        -v "$SECURE_DIR:/keys:ro" \
        stevef1uk/secure-packager:latest \
        issue-token -priv /keys/vendor_private.pem 2>&1)
    
    if [ $? -eq 0 ] && [ -n "$TEST_TOKEN" ]; then
        echo "   ✅ Token generation successful"
        echo "   📋 Test token preview: ${TEST_TOKEN:0:60}..."
        
        # Save test token
        echo "$TEST_TOKEN" > "$SECURE_DIR/token-test.txt"
        echo "   💾 Test token saved to $SECURE_DIR/token-test.txt"
        
        # Compare with existing token if it exists
        if [ -f "$SECURE_DIR/token.txt" ]; then
            LOCAL_TOKEN=$(cat "$SECURE_DIR/token.txt" | tr -d '\n\r ')
            if [ "$TEST_TOKEN" = "$LOCAL_TOKEN" ]; then
                echo "   ✅ Generated token matches existing token.txt"
            else
                echo "   ⚠️  Generated token differs from existing token.txt"
                echo "   💡 This is normal if token was regenerated or expired"
            fi
        fi
    else
        echo "   ❌ Token generation failed:"
        echo "$TEST_TOKEN"
        exit 1
    fi
else
    echo "   ⚠️  secure-packager image not available locally"
    echo "   💡 Pull it with: docker pull stevef1uk/secure-packager:latest"
fi

echo ""
echo "=========================================="
echo "Summary and Recommendations"
echo "=========================================="
echo ""
echo "If all checks passed, try:"
echo "  1. Update token: ./generate-vendor-token.sh $SECURE_DIR"
echo "  2. Update Kubernetes secret: ./update-secrets.sh $SECURE_DIR"
echo "  3. Restart the cronjob pod to pick up the new token"
echo ""
echo "To restart the cronjob:"
echo "  kubectl delete job -n agi -l job-name=wiki-bootstrapper-cronjob"
echo "  # Or wait for the next scheduled run"
echo ""






