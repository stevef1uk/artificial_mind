#!/bin/bash

# Force Goal Manager to use local image

NAMESPACE="${K8S_NAMESPACE:-agi}"

echo "🔧 Forcing Goal Manager to Use Local Image"
echo "=========================================="
echo ""

# Check current deployment
echo "📦 Current deployment image:"
CURRENT_IMAGE=$(kubectl get deployment goal-manager -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null)
CURRENT_POLICY=$(kubectl get deployment goal-manager -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].imagePullPolicy}' 2>/dev/null)
echo "   Image: $CURRENT_IMAGE"
echo "   Pull Policy: $CURRENT_POLICY"
echo ""

# Check if local image exists
echo "🔍 Checking for local image..."
LOCAL_IMAGES=$(docker images | grep "goal-manager\|stevef1uk/goal-manager" | head -5)
if [ -n "$LOCAL_IMAGES" ]; then
    echo "   Found local images:"
    echo "$LOCAL_IMAGES" | sed 's/^/      /'
else
    echo "   ⚠️  No local goal-manager images found"
    echo "   Run: ./test/rebuild_goal_manager.sh first"
    exit 1
fi
echo ""

# Get the most recent local image
LOCAL_IMAGE=$(docker images --format "{{.Repository}}:{{.Tag}}" | grep "goal-manager\|stevef1uk/goal-manager" | head -1)
if [ -z "$LOCAL_IMAGE" ]; then
    # Try to find any goal-manager image
    LOCAL_IMAGE=$(docker images goal-manager --format "{{.Repository}}:{{.Tag}}" | head -1)
fi

if [ -z "$LOCAL_IMAGE" ]; then
    echo "❌ Could not find local goal-manager image"
    exit 1
fi

echo "📦 Using local image: $LOCAL_IMAGE"
echo ""

# Tag it with the name Kubernetes expects
TARGET_IMAGE="stevef1uk/goal-manager:secure-local"
echo "🏷️  Tagging as: $TARGET_IMAGE"
docker tag "$LOCAL_IMAGE" "$TARGET_IMAGE" 2>/dev/null || echo "   (already tagged)"

echo ""
echo "🔄 Patching deployment..."
echo "------------------------"

# Patch image
kubectl set image deployment/goal-manager -n "$NAMESPACE" goal-manager="$TARGET_IMAGE"

# Patch imagePullPolicy to Never
kubectl patch deployment goal-manager -n "$NAMESPACE" --type='json' -p='[
  {"op": "replace", "path": "/spec/template/spec/containers/0/imagePullPolicy", "value": "Never"}
]'

echo ""
echo "⏳ Waiting for rollout..."
kubectl rollout status deployment/goal-manager -n "$NAMESPACE" --timeout=120s

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ Deployment updated successfully!"
    echo ""
    echo "📊 Verify the new pod:"
    NEW_POD=$(kubectl get pods -n "$NAMESPACE" -l app=goal-manager --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null)
    if [ -n "$NEW_POD" ]; then
        echo "   Pod: $NEW_POD"
        echo "   Image: $(kubectl get pod -n "$NAMESPACE" "$NEW_POD" -o jsonpath='{.spec.containers[0].image}')"
        echo "   Pull Policy: $(kubectl get pod -n "$NAMESPACE" "$NEW_POD" -o jsonpath='{.spec.containers[0].imagePullPolicy}')"
        echo ""
        echo "📝 Recent logs:"
        kubectl logs -n "$NAMESPACE" "$NEW_POD" --tail=10 | sed 's/^/      /'
    fi
else
    echo "❌ Rollout failed or timed out"
    echo "   Check: kubectl describe deployment goal-manager -n $NAMESPACE"
fi

