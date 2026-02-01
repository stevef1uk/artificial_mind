#!/bin/bash

# RPI / k3s version of test_artifact_creation.sh
# Uses kubectl port-forward to talk to the in-cluster HDN server.

set -euo pipefail

echo "🧪 HDN Artifact Creation Test (RPI / k3s)"
echo "========================================="
echo

# Configuration for k3s port-forward
API_URL="http://localhost:8081"
PORT_FORWARD_PID=""
ALT_PORT="18081"

# Simple colored output helpers (kept minimal)
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

print_info() {
  echo -e "${CYAN}ℹ️  $1${NC}"
}

print_success() {
  echo -e "${GREEN}✅ $1${NC}"
}

print_warning() {
  echo -e "${YELLOW}⚠️  $1${NC}"
}

print_error() {
  echo -e "${RED}❌ $1${NC}"
}

cleanup() {
  if [ -n "${PORT_FORWARD_PID:-}" ]; then
    kill "$PORT_FORWARD_PID" 2>/dev/null || true
    wait "$PORT_FORWARD_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

setup_port_forward() {
  if ! command -v kubectl >/dev/null 2>&1; then
    print_error "kubectl not found. Cannot set up port-forward."
    return 1
  fi

  if ! kubectl get svc -n agi hdn-server-rpi58 >/dev/null 2>&1; then
    print_error "HDN service 'hdn-server-rpi58' not found in namespace 'agi'"
    print_info "Available services in agi namespace:"
    kubectl get svc -n agi 2>/dev/null || echo "  (none found)"
    return 1
  fi

  # Prefer existing working port-forward
  EXISTING_PF=$(pgrep -f "kubectl.*port-forward.*hdn-server-rpi58.*8081" | head -1 || true)
  if [ -n "$EXISTING_PF" ]; then
    print_info "Found existing kubectl port-forward (PID: $EXISTING_PF)"
    if curl -s -f "$API_URL/health" >/dev/null 2>&1 || \
       curl -s -f "$API_URL/api/v1/health" >/dev/null 2>&1 || \
       curl -s -f -H "X-Request-Source: ui" "$API_URL/api/v1/intelligent/capabilities" >/dev/null 2>&1; then
      print_success "Existing port-forward is working"
      PORT_FORWARD_PID="$EXISTING_PF"
      return 0
    fi
    print_warning "Existing port-forward is not responding. Killing it…"
    kill "$EXISTING_PF" 2>/dev/null || true
    sleep 2
  fi

  # If 8081 is occupied by something else, try ALT_PORT
  if lsof -i :8081 >/dev/null 2>&1 || ss -tuln 2>/dev/null | grep -q ":8081 " || netstat -tuln 2>/dev/null | grep -q ":8081 " ; then
    if pgrep -f "kubectl.*port-forward.*8081" >/dev/null 2>&1; then
      print_info "Port 8081 in use by kubectl port-forward, verifying…"
      if curl -s -f -H "X-Request-Source: ui" "$API_URL/api/v1/intelligent/capabilities" >/dev/null 2>&1; then
        EXISTING_PF=$(pgrep -f "kubectl.*port-forward.*8081" | head -1 || true)
        if [ -n "$EXISTING_PF" ]; then
          PORT_FORWARD_PID="$EXISTING_PF"
          print_success "Existing port-forward is working (PID: $PORT_FORWARD_PID)"
          return 0
        fi
      fi
    fi
    print_warning "Port 8081 is in use by a non-kubectl process"
    print_info "Attempting to use existing connection if it works…"
    if curl -s -f -H "X-Request-Source: ui" "$API_URL/api/v1/intelligent/capabilities" >/dev/null 2>&1; then
      print_success "Port 8081 is accessible and working"
      return 0
    fi

    print_warning "Port 8081 is in use and not responding to HDN API calls"
    print_info "Trying alternative port $ALT_PORT…"
    if lsof -i :"$ALT_PORT" >/dev/null 2>&1; then
      print_error "Alternative port $ALT_PORT is also in use"
      return 1
    fi
    API_URL="http://localhost:$ALT_PORT"
    print_info "Using alternative port $ALT_PORT for port-forward"
  fi

  LOCAL_PORT=$(echo "$API_URL" | sed -n 's|.*:\([0-9]*\)$|\1|p')
  [ -z "$LOCAL_PORT" ] && LOCAL_PORT=8081

  print_info "Setting up kubectl port-forward to HDN service on port $LOCAL_PORT…"
  kubectl port-forward -n agi svc/hdn-server-rpi58 "$LOCAL_PORT":8080 >/tmp/hdn-port-forward.log 2>&1 &
  PORT_FORWARD_PID=$!
  sleep 3

  if ! kill -0 "$PORT_FORWARD_PID" 2>/dev/null; then
    print_error "Port-forward process died immediately"
    print_info "Check logs: cat /tmp/hdn-port-forward.log"
    PORT_FORWARD_PID=""
    return 1
  fi

  print_info "Testing connectivity to HDN service…"
  for attempt in 1 2 3 4 5; do
    if curl -s -f "$API_URL/health" >/dev/null 2>&1 || \
       curl -s -f "$API_URL/api/v1/health" >/dev/null 2>&1 || \
       curl -s -f -H "X-Request-Source: ui" "$API_URL/api/v1/intelligent/capabilities" >/dev/null 2>&1; then
      print_success "Port-forward established and verified (PID: $PORT_FORWARD_PID)"
      return 0
    fi
    sleep 1
  done

  print_error "Port-forward is running but service is not responding"
  print_info "Port-forward PID: $PORT_FORWARD_PID"
  print_info "Check if HDN pod is running: kubectl get pods -n agi -l app=hdn-server-rpi58"
  print_info "Port-forward logs: cat /tmp/hdn-port-forward.log"
  kill "$PORT_FORWARD_PID" 2>/dev/null || true
  PORT_FORWARD_PID=""
  return 1
}

if ! setup_port_forward; then
  exit 1
fi

# After port-forward, reuse the original test_artifact_creation.sh logic,
# but point HDN_URL at API_URL.

HDN_URL="${HDN_URL:-$API_URL}"

# Use project-relative temp directory for portability
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TMP_DIR="$PROJECT_ROOT/tmp"
mkdir -p "$TMP_DIR"

echo
echo "Using HDN_URL=$HDN_URL"
echo

# ------------------------------------------------------------
# Automatic Chained flow: Python -> JSON -> Go (via single request)
# This block is copied from test_artifact_creation.sh (A/… and C/… sections)
# ------------------------------------------------------------

echo ""
echo "[C/1] Testing automatic chained execution with single request..."
CHAINED_REQ='{
  "task_name": "chained_programs",
  "description": "Create TWO programs executed sequentially. Program 1 (Python) must PRINT EXACTLY one line with the JSON string {\"number\": 21} (use the key '\''number'\'' exactly, not '\''nomenclature'\'' or any other key) and no other output. Program 2 (Go) must READ the previous JSON from stdin, extract the '\''number'\'' field (the JSON key must be '\''number'\''), multiply it by 2, and print ONLY the number 42 (no extra text, no labels, no JSON, no quotes). Do NOT print any extra whitespace, labels, prompts, or commentary in either program. The JSON key MUST be '\''number'\'' not '\''nomenclature'\'' or any variation.",
  "context": {
    "artifacts_wrapper": "true",
    "artifact_names": "prog1.py,prog2.go"
  },
  "language": "python",
  "priority": "high"
}'

CHAINED_JSON="$TMP_DIR/int_exec_chained.json"
curl -s --max-time 120 -X POST "$HDN_URL/api/v1/intelligent/execute" \
  -H 'Content-Type: application/json' \
  -H 'X-Request-Source: ui' \
  --data-binary "$CHAINED_REQ" \
  -o "$CHAINED_JSON"

if ! jq -e '.success == true' "$CHAINED_JSON" >/dev/null; then
  echo "ERROR: chained execution failed:" >&2
  jq . "$CHAINED_JSON" || cat "$CHAINED_JSON"
  exit 1
fi

CHAINED_WID=$(jq -r '.workflow_id' "$CHAINED_JSON")
echo "[C/2] Chained workflow: $CHAINED_WID"

CHAINED_FILES_JSON="$TMP_DIR/wf_files_chained.json"
curl -s -H "X-Request-Source: ui" "$HDN_URL/api/v1/files/workflow/$CHAINED_WID" -o "$CHAINED_FILES_JSON"

if ! jq -e 'map(.filename) | index("prog1.py") != null' "$CHAINED_FILES_JSON" >/dev/null; then
  echo "ERROR: prog1.py not found in generated files" >&2
  jq '.files[].filename' "$CHAINED_FILES_JSON" >&2 || true
  exit 1
fi

if ! jq -e 'map(.filename) | index("prog2.go") != null' "$CHAINED_FILES_JSON" >/dev/null; then
  echo "ERROR: prog2.go not found in generated files" >&2
  jq '.files[].filename' "$CHAINED_FILES_JSON" >&2 || true
  exit 1
fi

# Verify the generated code uses the correct JSON key "number"
GENERATED_CODE=$(jq -r '.generated_code.code' "$CHAINED_JSON")
if echo "$GENERATED_CODE" | grep -q '"nomenclature"'; then
  echo "WARNING: Generated code uses 'nomenclature' instead of 'number' - this may cause the test to fail" >&2
  echo "The code should use '\"number\"' as the JSON key, not '\"nomenclature\"'" >&2
fi

RES=$(jq -r '.result' "$CHAINED_JSON")
echo "[C/3] Chained result: $RES"

# Check if result contains 42 (allowing for whitespace/newlines)
if echo "$RES" | grep -qE '^42$|^42[[:space:]]*$|[[:space:]]*42[[:space:]]*$'; then
  echo "PASS: Found expected result '42'"
else
  # Also check validation steps output as fallback
  VALIDATION_OUTPUT=$(jq -r '.validation_steps[]?.output // empty' "$CHAINED_JSON" | grep -E '^42$|^42[[:space:]]*$' | head -1)
  if [ -n "$VALIDATION_OUTPUT" ]; then
    echo "PASS: Found expected result '42' in validation output"
  else
    echo "ERROR: expected '42' in chained result" >&2
    echo "Result was: '$RES'" >&2
    echo "Full response:" >&2
    jq . "$CHAINED_JSON" || cat "$CHAINED_JSON"
    exit 1
  fi
fi

echo "PASS: Automatic chained flow verified (Python -> JSON -> Go -> 42)"


