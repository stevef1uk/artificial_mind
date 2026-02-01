#!/bin/bash

# RPI / k3s version of test_chained_programs.sh
# Uses kubectl port-forward to talk to the in-cluster HDN server.

set -euo pipefail

echo "🧪 HDN Chained Programs Test (RPI / k3s)"
echo "========================================"
echo

API_URL="http://localhost:8081"
PORT_FORWARD_PID=""
ALT_PORT="18081"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

print_info()  { echo -e "${CYAN}ℹ️  $1${NC}"; }
print_ok()    { echo -e "${GREEN}✅ $1${NC}"; }
print_warn()  { echo -e "${YELLOW}⚠️  $1${NC}"; }
print_error() { echo -e "${RED}❌ $1${NC}"; }

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

  EXISTING_PF=$(pgrep -f "kubectl.*port-forward.*hdn-server-rpi58.*8081" | head -1 || true)
  if [ -n "$EXISTING_PF" ]; then
    print_info "Found existing kubectl port-forward (PID: $EXISTING_PF)"
    if curl -s -f -H "X-Request-Source: ui" "$API_URL/api/v1/intelligent/capabilities" >/dev/null 2>&1; then
      print_ok "Existing port-forward is working"
      PORT_FORWARD_PID="$EXISTING_PF"
      return 0
    fi
    print_warn "Existing port-forward is not responding. Killing it…"
    kill "$EXISTING_PF" 2>/dev/null || true
    sleep 2
  fi

  if lsof -i :8081 >/dev/null 2>&1 || ss -tuln 2>/dev/null | grep -q ":8081 " || netstat -tuln 2>/dev/null | grep -q ":8081 " ; then
    if pgrep -f "kubectl.*port-forward.*8081" >/dev/null 2>&1; then
      print_info "Port 8081 in use by kubectl port-forward, verifying…"
      if curl -s -f -H "X-Request-Source: ui" "$API_URL/api/v1/intelligent/capabilities" >/dev/null 2>&1; then
        EXISTING_PF=$(pgrep -f "kubectl.*port-forward.*8081" | head -1 || true)
        if [ -n "$EXISTING_PF" ]; then
          PORT_FORWARD_PID="$EXISTING_PF"
          print_ok "Existing port-forward is working (PID: $PORT_FORWARD_PID)"
          return 0
        fi
      fi
    fi
    print_warn "Port 8081 is in use by a non-kubectl process"
    print_info "Attempting to use existing connection if it works…"
    if curl -s -f -H "X-Request-Source: ui" "$API_URL/api/v1/intelligent/capabilities" >/dev/null 2>&1; then
      print_ok "Port 8081 is accessible and working"
      return 0
    fi

    print_warn "Port 8081 is in use and not responding to HDN API calls"
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
    if curl -s -f -H "X-Request-Source: ui" "$API_URL/api/v1/intelligent/capabilities" >/dev/null 2>&1 || \
       curl -s -f "$API_URL/health" >/dev/null 2>&1 || \
       curl -s -f "$API_URL/api/v1/health" >/dev/null 2>&1; then
      print_ok "Port-forward established and verified (PID: $PORT_FORWARD_PID)"
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

HDN_URL="$API_URL"

echo
print_info "Using HDN_URL=$HDN_URL"
echo

echo "🧪 Testing Chained Program Execution (RPI / k3s)"
echo "-----------------------------------------------"
echo

echo "Test 1: Python generates JSON, Go reads it"
echo "-------------------------------------------"

REQUEST='{
  "task_name": "chained_programs",
  "description": "Create TWO programs executed sequentially. Program 1 (Python) must PRINT EXACTLY one line with the JSON string {\"number\": 21} and no other output. Program 2 (Go) must READ the previous JSON from stdin, extract the '\''number'\'' field, multiply it by 2, and print the result (no extra text, no labels, no JSON). Do NOT print any extra whitespace, labels, prompts, or commentary in either program.",
  "context": {
    "artifacts_wrapper": "true",
    "artifact_names": "prog1.py,prog2.go"
  },
  "language": "python",
  "priority": "high"
}'

echo "Sending request..."
RESPONSE=$(curl -s --max-time 120 -X POST "$HDN_URL/api/v1/intelligent/execute" \
  -H 'Content-Type: application/json' \
  -H 'X-Request-Source: ui' \
  -d "$REQUEST")

echo "Response:"
echo "$RESPONSE" | jq '.' || echo "$RESPONSE"

SUCCESS=$(echo "$RESPONSE" | jq -r '.success // false')
WORKFLOW_ID=$(echo "$RESPONSE" | jq -r '.workflow_id // ""')

if [ "$SUCCESS" = "true" ] && [ -n "$WORKFLOW_ID" ]; then
  echo
  print_ok "Request successful! Workflow ID: $WORKFLOW_ID"
  echo
  echo "Checking generated files..."

  FILES_RESPONSE=$(curl -s -H "X-Request-Source: ui" "$HDN_URL/api/v1/files/workflow/$WORKFLOW_ID")
  echo "$FILES_RESPONSE" | jq '.[] | {filename: .filename, size: .size}' || echo "$FILES_RESPONSE"

  HAS_PROG1=$(echo "$FILES_RESPONSE" | jq -r '.[]? | select(.filename == "prog1.py") | .filename' || echo "")
  HAS_PROG2=$(echo "$FILES_RESPONSE" | jq -r '.[]? | select(.filename == "prog2.go") | .filename' || echo "")

  if [ -n "$HAS_PROG1" ] && [ -n "$HAS_PROG2" ]; then
    echo
    print_ok "Both files generated!"
    echo
    echo "prog1.py content:"
    # Try workflow-specific endpoint first, fallback to filename-based endpoint
    FILE1_CONTENT=$(curl -s -H "X-Request-Source: ui" "$HDN_URL/api/v1/workflow/$WORKFLOW_ID/files/prog1.py" 2>&1)
    if echo "$FILE1_CONTENT" | grep -q "File not found\|Workflow not found"; then
      # Fallback to filename-based endpoint (uses file storage)
      FILE1_CONTENT=$(curl -s -H "X-Request-Source: ui" "$HDN_URL/api/v1/files/prog1.py" 2>&1)
    fi
    echo "$FILE1_CONTENT" | head -30
    echo
    echo "prog2.go content:"
    # Try workflow-specific endpoint first, fallback to filename-based endpoint
    FILE2_CONTENT=$(curl -s -H "X-Request-Source: ui" "$HDN_URL/api/v1/workflow/$WORKFLOW_ID/files/prog2.go" 2>&1)
    if echo "$FILE2_CONTENT" | grep -q "File not found\|Workflow not found"; then
      # Fallback to filename-based endpoint (uses file storage)
      FILE2_CONTENT=$(curl -s -H "X-Request-Source: ui" "$HDN_URL/api/v1/files/prog2.go" 2>&1)
    fi
    echo "$FILE2_CONTENT" | head -30
  else
    print_warn "Missing files:"
    [ -z "$HAS_PROG1" ] && echo "  - prog1.py not found"
    [ -z "$HAS_PROG2" ] && echo "  - prog2.go not found"
  fi
else
  echo
  print_error "Request failed or no workflow ID"
  ERROR=$(echo "$RESPONSE" | jq -r '.error // "Unknown error"')
  echo "Error: $ERROR"
  exit 1
fi


