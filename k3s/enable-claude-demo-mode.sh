#!/bin/bash
# =============================================================================
# enable-claude-demo-mode.sh
# Switches the LLM to cheapest Claude (claude-3-haiku-20240307) and disables
# all background processing to minimise API costs during a demo.
#
# Usage:
#   ./k3s/enable-claude-demo-mode.sh <your-claude-api-key>
#
# To restore original settings:
#   ./k3s/disable-claude-demo-mode.sh
# =============================================================================

set -e

NAMESPACE="agi"
CLAUDE_API_KEY="${1:-}"

# ─── Validate ─────────────────────────────────────────────────────────────────
if [ -z "$CLAUDE_API_KEY" ]; then
  echo "❌  Usage: $0 <your-claude-api-key>"
  echo "   Example: $0 sk-ant-api03-..."
  exit 1
fi

echo "╔══════════════════════════════════════════════════════╗"
echo "║          Enabling Claude Demo Mode                   ║"
echo "╚══════════════════════════════════════════════════════╝"
echo ""

# ─── 1. Patch LLM config secret → cheapest Claude model ───────────────────────
echo "▶ [1/5] Patching llm-config secret → anthropic / claude-3-haiku-20240307 ..."

kubectl patch secret llm-config -n "$NAMESPACE" -p "{
  \"stringData\": {
    \"LLM_PROVIDER\": \"anthropic\",
    \"LLM_MODEL\": \"claude-3-haiku-20240307\",
    \"LLM_API_KEY\": \"$CLAUDE_API_KEY\",
    \"USE_ASYNC_LLM_QUEUE\": \"0\",
    \"USE_ASYNC_HTTP_QUEUE\": \"0\"
  }
}"

echo "   ✅ Secret patched."

# ─── 2. Suspend all background cronjobs ───────────────────────────────────────
echo ""
echo "▶ [2/5] Suspending background cronjobs ..."

for CRONJOB in news-ingestor-cronjob wiki-summarizer-cronjob wiki-bootstrapper-cronjob; do
  if kubectl get cronjob "$CRONJOB" -n "$NAMESPACE" &>/dev/null; then
    kubectl patch cronjob "$CRONJOB" -n "$NAMESPACE" -p '{"spec":{"suspend":true}}'
    echo "   ✅ Suspended: $CRONJOB"
  else
    echo "   ⚠️  Not found (skipping): $CRONJOB"
  fi
done

# ─── 3. Disable background LLM via Redis flag ─────────────────────────────────
echo ""
echo "▶ [3/5] Setting DISABLE_BACKGROUND_LLM flag in Redis ..."

REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$REDIS_POD" ]; then
  kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli SET "DISABLE_BACKGROUND_LLM" "1" > /dev/null
  echo "   ✅ Redis flag set."
else
  echo "   ⚠️  Redis pod not found — skipping Redis flag (non-fatal)."
fi

# ─── 4. Restart HDN and FSM deployments to pick up new secret ─────────────────
echo ""
echo "▶ [4/5] Patching deployments and restarting services ..."
echo "🛠 Patching deployments to map LLM_API_KEY from secret..."
kubectl patch deployment hdn-server-rpi58 -n "$NAMESPACE" --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/env/-", "value": {"name": "LLM_API_KEY", "valueFrom": {"secretKeyRef": {"name": "llm-config", "key": "LLM_API_KEY"}}}}]' 2>/dev/null || \
kubectl patch deployment hdn-server-rpi58 -n "$NAMESPACE" --patch '{"spec":{"template":{"spec":{"containers":[{"name":"hdn-server","env":[{"name":"LLM_API_KEY","valueFrom":{"secretKeyRef":{"name":"llm-config","key":"LLM_API_KEY"}}}]}]}}}}'

kubectl patch deployment hdn-server-rpi58 -n "$NAMESPACE" --patch '{"spec":{"template":{"spec":{"containers":[{"name":"hdn-server","env":[{"name":"DISABLE_BACKGROUND_LLM","value":"1"}]}]}}}}'

kubectl patch deployment fsm-server-rpi58 -n "$NAMESPACE" --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/env/-", "value": {"name": "LLM_API_KEY", "valueFrom": {"secretKeyRef": {"name": "llm-config", "key": "LLM_API_KEY"}}}}]' 2>/dev/null || \
kubectl patch deployment fsm-server-rpi58 -n "$NAMESPACE" --patch '{"spec":{"template":{"spec":{"containers":[{"name":"fsm-server","env":[{"name":"LLM_API_KEY","valueFrom":{"secretKeyRef":{"name":"llm-config","key":"LLM_API_KEY"}}}]}]}}}}'

kubectl patch deployment fsm-server-rpi58 -n "$NAMESPACE" --patch '{"spec":{"template":{"spec":{"containers":[{"name":"fsm-server","env":[{"name":"DISABLE_BACKGROUND_LLM","value":"1"}]}]}}}}'


# Restart services
echo "♻️ Restarting services..."
kubectl rollout restart deployment hdn-server-rpi58 -n "$NAMESPACE"
kubectl rollout restart deployment fsm-server-rpi58 -n "$NAMESPACE"

echo "   ✅ Deployments patched and restarted."

# ─── 5. Wait for rollout ──────────────────────────────────────────────────────
echo ""
echo "▶ [5/5] Waiting for rollouts to complete ..."

for DEPLOY in hdn-server-rpi58 fsm-server-rpi58; do
  if kubectl get deployment "$DEPLOY" -n "$NAMESPACE" &>/dev/null; then
    kubectl rollout status deployment "$DEPLOY" -n "$NAMESPACE" --timeout=120s && \
      echo "   ✅ $DEPLOY is ready." || \
      echo "   ⚠️  $DEPLOY rollout timed out — check with: kubectl get pods -n $NAMESPACE"
  fi
done

# ─── Summary ──────────────────────────────────────────────────────────────────
echo ""
echo "╔══════════════════════════════════════════════════════╗"
echo "║  ✅  Claude Demo Mode ENABLED                        ║"
echo "╠══════════════════════════════════════════════════════╣"
echo "║  Model    : claude-3-haiku-20240307 (cheapest)       ║"
echo "║  Provider : anthropic                                ║"
echo "║  Async Q  : DISABLED                                 ║"
echo "║  CronJobs : SUSPENDED (news, wiki-summarizer, wiki-  ║"
echo "║             bootstrapper)                            ║"
echo "║  BG LLM   : DISABLED (Redis flag)                    ║"
echo "╠══════════════════════════════════════════════════════╣"
echo "║  To restore: ./k3s/disable-claude-demo-mode.sh       ║"
echo "╚══════════════════════════════════════════════════════╝"
