#!/bin/bash
# =============================================================================
# disable-claude-demo-mode.sh
# Restores the LLM config back to Ollama/llama3.2 and re-enables all
# background processing that was suspended by enable-claude-demo-mode.sh
#
# Usage:
#   ./k3s/disable-claude-demo-mode.sh
# =============================================================================

set -e

NAMESPACE="agi"

echo "╔══════════════════════════════════════════════════════╗"
echo "║          Disabling Claude Demo Mode                  ║"
echo "║          (Restoring original configuration)          ║"
echo "╚══════════════════════════════════════════════════════╝"
echo ""

# ─── 1. Restore LLM config secret → Ollama ────────────────────────────────────
echo "▶ [1/5] Restoring llm-config secret → ollama / llama3.2:latest ..."

kubectl patch secret llm-config -n "$NAMESPACE" -p "{
  \"stringData\": {
    \"LLM_PROVIDER\": \"ollama\",
    \"LLM_MODEL\": \"llama3.2:latest\",
    \"OPENAI_BASE_URL\": \"http://192.168.1.53:11434\",
    \"USE_ASYNC_LLM_QUEUE\": \"1\",
    \"USE_ASYNC_HTTP_QUEUE\": \"1\"
  }
}"

# Remove the API key entry (requires JSON patch for deletion)
kubectl patch secret llm-config -n "$NAMESPACE" --type='json' \
  -p='[{"op": "remove", "path": "/data/LLM_API_KEY"}]' 2>/dev/null && \
  echo "   ✅ LLM_API_KEY removed from secret." || \
  echo "   ℹ️  LLM_API_KEY was not in secret — nothing to remove."

echo "   ✅ Secret restored to Ollama."

# ─── 2. Unsuspend all background cronjobs ─────────────────────────────────────
echo ""
echo "▶ [2/5] Re-enabling background cronjobs ..."

for CRONJOB in news-ingestor-cronjob wiki-summarizer-cronjob wiki-bootstrapper-cronjob; do
  if kubectl get cronjob "$CRONJOB" -n "$NAMESPACE" &>/dev/null; then
    kubectl patch cronjob "$CRONJOB" -n "$NAMESPACE" -p '{"spec":{"suspend":false}}'
    echo "   ✅ Resumed: $CRONJOB"
  else
    echo "   ⚠️  Not found (skipping): $CRONJOB"
  fi
done

# ─── 3. Clear background LLM Redis flag ───────────────────────────────────────
echo ""
echo "▶ [3/5] Clearing DISABLE_BACKGROUND_LLM flag in Redis ..."

REDIS_POD=$(kubectl get pods -n "$NAMESPACE" -l app=redis -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
if [ -n "$REDIS_POD" ]; then
  kubectl exec -n "$NAMESPACE" "$REDIS_POD" -- redis-cli DEL "DISABLE_BACKGROUND_LLM" > /dev/null
  echo "   ✅ Redis flag cleared."
else
  echo "   ⚠️  Redis pod not found — skipping (non-fatal)."
fi

# ─── 4. Restart HDN and FSM deployments ───────────────────────────────────────
echo ""
echo "▶ [4/5] Restarting HDN and FSM deployments ..."

# Patch deployments to set DISABLE_BACKGROUND_LLM=0
echo "▶ Patching deployments to re-enable background LLM processing..."
kubectl patch deployment hdn-server-rpi58 -n "$NAMESPACE" --patch '{"spec":{"template":{"spec":{"containers":[{"name":"hdn-server","env":[{"name":"DISABLE_BACKGROUND_LLM","value":"0"}]}]}}}}'
kubectl patch deployment fsm-server-rpi58 -n "$NAMESPACE" --patch '{"spec":{"template":{"spec":{"containers":[{"name":"fsm-server","env":[{"name":"DISABLE_BACKGROUND_LLM","value":"0"}]}]}}}}'
echo "   ✅ Deployments patched."

# Restart services
echo "♻️ Restarting services..."
kubectl rollout restart deployment hdn-server-rpi58 -n "$NAMESPACE"
kubectl rollout restart deployment fsm-server-rpi58 -n "$NAMESPACE"
echo "   ✅ Restart commands issued."

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
echo "║  ✅  Claude Demo Mode DISABLED — config restored     ║"
echo "╠══════════════════════════════════════════════════════╣"
echo "║  Model    : llama3.2:latest                          ║"
echo "║  Provider : ollama (192.168.1.53:11434)              ║"
echo "║  Async Q  : ENABLED                                  ║"
echo "║  CronJobs : RESUMED (news, wiki-summarizer, wiki-    ║"
echo "║             bootstrapper)                            ║"
echo "║  BG LLM   : ENABLED (Redis flag cleared)             ║"
echo "╚══════════════════════════════════════════════════════╝"
