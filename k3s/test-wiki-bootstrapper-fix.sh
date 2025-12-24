#!/bin/bash

# Test script to diagnose why wiki-bootstrapper token signature fix hasn't worked

set -e

NAMESPACE="agi"
IMAGE_NAME="stevef1uk/knowledge-builder:secure"

echo "=========================================="
echo "Wiki-Bootstrapper Fix Diagnostic"
echo "=========================================="
echo ""

# 1. Check Dockerfile was fixed
echo "1. Checking Dockerfile.wiki-bootstrapper.secure..."
if grep -q "\-zip=true" ../Dockerfile.wiki-bootstrapper.secure; then
    echo "   ✅ Dockerfile has -zip=true flag"
else
    echo "   ❌ Dockerfile is missing -zip=true flag!"
    exit 1
fi
echo ""

# 2. Check if image exists locally
echo "2. Checking local Docker image..."
if docker image inspect "$IMAGE_NAME" >/dev/null 2>&1; then
    LOCAL_IMAGE_DATE=$(docker image inspect "$IMAGE_NAME" --format '{{.Created}}' 2>/dev/null || echo "unknown")
    echo "   ✅ Local image exists (created: $LOCAL_IMAGE_DATE)"
    
    # Check image size
    IMAGE_SIZE=$(docker image inspect "$IMAGE_NAME" --format '{{.Size}}' 2>/dev/null | numfmt --to=iec-i --suffix=B 2>/dev/null || echo "unknown")
    echo "   📦 Image size: $IMAGE_SIZE"
else
    echo "   ⚠️  Local image not found"
fi
echo ""

# 3. Check what image Kubernetes is using
echo "3. Checking Kubernetes pod image..."
LATEST_POD=$(kubectl get pods -n $NAMESPACE -l app=wiki-bootstrapper --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null || echo "")

if [ -n "$LATEST_POD" ]; then
    K8S_IMAGE=$(kubectl get pod "$LATEST_POD" -n $NAMESPACE -o jsonpath='{.spec.containers[0].image}' 2>/dev/null || echo "unknown")
    echo "   📋 Latest pod: $LATEST_POD"
    echo "   🖼️  Image: $K8S_IMAGE"
    
    if [ "$K8S_IMAGE" = "$IMAGE_NAME" ]; then
        echo "   ✅ Pod is using correct image name"
    else
        echo "   ⚠️  Pod is using different image: $K8S_IMAGE"
    fi
    
    # Check image pull policy
    PULL_POLICY=$(kubectl get pod "$LATEST_POD" -n $NAMESPACE -o jsonpath='{.spec.containers[0].imagePullPolicy}' 2>/dev/null || echo "unknown")
    echo "   🔄 Image pull policy: $PULL_POLICY"
    
    # Check pod creation time
    POD_AGE=$(kubectl get pod "$LATEST_POD" -n $NAMESPACE -o jsonpath='{.metadata.creationTimestamp}' 2>/dev/null || echo "unknown")
    echo "   🕐 Pod created: $POD_AGE"
else
    echo "   ⚠️  No wiki-bootstrapper pods found"
fi
echo ""

# 4. Check if image was pushed to Docker Hub
echo "4. Checking Docker Hub for latest image..."
if command -v docker >/dev/null 2>&1; then
    echo "   🔍 Attempting to check remote image..."
    # Try to pull and inspect
    if docker pull "$IMAGE_NAME" >/dev/null 2>&1; then
        REMOTE_DATE=$(docker image inspect "$IMAGE_NAME" --format '{{.Created}}' 2>/dev/null || echo "unknown")
        echo "   ✅ Remote image exists (created: $REMOTE_DATE)"
        
        # Compare local vs remote
        if [ -n "$LOCAL_IMAGE_DATE" ] && [ "$LOCAL_IMAGE_DATE" != "unknown" ]; then
            if [ "$LOCAL_IMAGE_DATE" = "$REMOTE_DATE" ]; then
                echo "   ✅ Local and remote images match"
            else
                echo "   ⚠️  Local and remote images differ!"
                echo "      Local:  $LOCAL_IMAGE_DATE"
                echo "      Remote: $REMOTE_DATE"
            fi
        fi
    else
        echo "   ⚠️  Could not pull remote image (may need docker login)"
    fi
else
    echo "   ⚠️  Docker not available for remote check"
fi
echo ""

# 5. Check cronjob configuration
echo "5. Checking cronjob configuration..."
if kubectl get cronjob wiki-bootstrapper-cronjob -n $NAMESPACE >/dev/null 2>&1; then
    CRONJOB_IMAGE=$(kubectl get cronjob wiki-bootstrapper-cronjob -n $NAMESPACE -o jsonpath='{.spec.jobTemplate.spec.template.spec.containers[0].image}' 2>/dev/null || echo "unknown")
    CRONJOB_PULL_POLICY=$(kubectl get cronjob wiki-bootstrapper-cronjob -n $NAMESPACE -o jsonpath='{.spec.jobTemplate.spec.template.spec.containers[0].imagePullPolicy}' 2>/dev/null || echo "unknown")
    echo "   📋 CronJob image: $CRONJOB_IMAGE"
    echo "   🔄 CronJob pull policy: $CRONJOB_PULL_POLICY"
    
    if [ "$CRONJOB_IMAGE" = "$IMAGE_NAME" ]; then
        echo "   ✅ CronJob is configured with correct image"
    else
        echo "   ⚠️  CronJob image differs: $CRONJOB_IMAGE"
    fi
    
    if [ "$CRONJOB_PULL_POLICY" = "Always" ]; then
        echo "   ✅ Pull policy is 'Always' - will pull new images"
    elif [ "$CRONJOB_PULL_POLICY" = "IfNotPresent" ]; then
        echo "   ⚠️  Pull policy is 'IfNotPresent' - may use cached image"
    fi
else
    echo "   ❌ CronJob not found"
fi
echo ""

# 6. Check recent pod logs for errors
echo "6. Checking recent pod logs..."
if [ -n "$LATEST_POD" ]; then
    echo "   📋 Last 20 lines from pod $LATEST_POD:"
    kubectl logs "$LATEST_POD" -n $NAMESPACE --tail=20 2>&1 | sed 's/^/      /' || echo "      Could not retrieve logs"
else
    echo "   ⚠️  No pod available for log check"
fi
echo ""

# 7. Test local image if available
echo "7. Testing local image (if available)..."
if docker image inspect "$IMAGE_NAME" >/dev/null 2>&1; then
    echo "   🧪 Attempting to inspect image layers..."
    # Check if we can see the encrypted file
    if docker run --rm "$IMAGE_NAME" ls -la /app/*.enc 2>/dev/null | grep -q "knowledge-builder.enc"; then
        echo "   ✅ Image contains knowledge-builder.enc"
    else
        echo "   ⚠️  Could not verify encrypted file in image"
    fi
else
    echo "   ⚠️  Local image not available for testing"
fi
echo ""

# 8. Recommendations
echo "=========================================="
echo "Diagnosis Summary"
echo "=========================================="
echo ""

if [ "$CRONJOB_PULL_POLICY" != "Always" ]; then
    echo "🔴 ISSUE FOUND: Image pull policy is not 'Always'"
    echo "   Fix: Update cronjob to use imagePullPolicy: Always"
    echo "   Or: Delete existing pods to force image pull"
    echo ""
fi

if [ -n "$LATEST_POD" ] && [ -n "$POD_AGE" ]; then
    # Check if pod is older than 1 hour (assuming image was just rebuilt)
    echo "📋 Latest pod was created: $POD_AGE"
    echo "   If you just rebuilt the image, you may need to:"
    echo "   1. Delete the cronjob jobs: kubectl delete job -n $NAMESPACE -l job-name=wiki-bootstrapper-cronjob"
    echo "   2. Wait for next scheduled run, or manually trigger"
    echo ""
fi

echo "Next steps:"
echo "  1. Verify image was rebuilt: docker images | grep knowledge-builder"
echo "  2. Verify image was pushed: docker push $IMAGE_NAME"
echo "  3. Force pod recreation: kubectl delete job -n $NAMESPACE -l job-name=wiki-bootstrapper-cronjob"
echo "  4. Check new pod logs after recreation"
echo ""

