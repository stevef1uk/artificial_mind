#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${1:-agi}"

echo "[tools-refresh] Using namespace: ${NAMESPACE}"

# Find an hdn-server pod
POD_NAME=$(kubectl -n "${NAMESPACE}" get pods -l app=hdn-server -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [[ -z "${POD_NAME}" ]]; then
  echo "[tools-refresh] ERROR: No hdn-server pod found in namespace ${NAMESPACE}" >&2
  exit 1
fi

echo "[tools-refresh] Target pod: ${POD_NAME}"

# Trigger discovery
echo "[tools-refresh] Triggering tool discovery..."
kubectl -n "${NAMESPACE}" exec "${POD_NAME}" -- sh -lc "wget -qO- --post-data='' --header=Content-Type:application/json http://127.0.0.1:8080/api/v1/tools/discover" || {
  echo "[tools-refresh] WARN: Discovery call failed" >&2
}

echo
echo "[tools-refresh] Fetching registered tools (first 1200 chars)..."
kubectl -n "${NAMESPACE}" exec "${POD_NAME}" -- sh -lc "wget -qO- http://127.0.0.1:8080/api/v1/tools/list | head -c 1200; echo" || {
  echo "[tools-refresh] WARN: List call failed" >&2
}

echo "[tools-refresh] Done."


