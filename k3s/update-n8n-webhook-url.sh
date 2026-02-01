#!/bin/bash
# Script to update N8N_WEBHOOK_URL in llm-config secret

set -e

PRODUCTION_URL="https://k3s.sjfisher.com/webhook/6f632b61-6b01-4910-991d-3a378b1e653a"
NAMESPACE="agi"
SECRET_NAME="llm-config"

echo "🔧 Updating N8N_WEBHOOK_URL in ${SECRET_NAME} secret..."
echo "   URL: ${PRODUCTION_URL}"
echo ""

# Check if secret exists
if ! kubectl get secret ${SECRET_NAME} -n ${NAMESPACE} >/dev/null 2>&1; then
    echo "⚠️  Secret ${SECRET_NAME} not found. Creating it first..."
    kubectl apply -f k3s/llm-config-secret.yaml
fi

# Update the secret
kubectl patch secret ${SECRET_NAME} -n ${NAMESPACE} --type='json' \
  -p="[{\"op\": \"add\", \"path\": \"/data/N8N_WEBHOOK_URL\", \"value\": \"$(echo -n ${PRODUCTION_URL} | base64 | tr -d '\n')\"}]"

echo "✅ Secret updated successfully!"
echo ""
echo "🔄 Restarting HDN server to pick up changes..."
kubectl rollout restart deployment hdn-server-rpi58 -n ${NAMESPACE}

echo ""
echo "✅ Done! HDN server will restart and use the new webhook URL."

