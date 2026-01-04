#!/bin/bash

# Quick deploy script for FSM with updated code
# Usage: ./deploy-fsm.sh

set -e

NAMESPACE="agi"
DEPLOYMENT="fsm-server-rpi58"

echo "🔨 Building FSM binary..."
cd "$(dirname "$0")/fsm"
go build -o fsm 2>&1 | tail -5

echo "✅ Build complete"
echo ""
echo "📦 To deploy the updated FSM to Kubernetes, run:"
echo "  kubectl rollout restart deployment/$DEPLOYMENT -n $NAMESPACE"
echo ""
echo "⚠️  Note: You'll need to rebuild the Docker image and push to registry for persistent deployment:"
echo "  docker build -f Dockerfile.fsm.secure -t stevef1uk/fsm-server:secure ."
echo "  docker push stevef1uk/fsm-server:secure"
