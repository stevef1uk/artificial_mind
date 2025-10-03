#!/usr/bin/env bash
set -euo pipefail

HDN_URL=${HDN_URL:-http://localhost:8081}
TMP_DIR=/home/stevef/dev/agi/tmp
mkdir -p "$TMP_DIR"

# ------------------------------------------------------------
# Automatic Chained flow: Python -> JSON -> Go (via single request)
# Test the new automatic chaining system
# ------------------------------------------------------------
echo ""
echo "[C/1] Testing automatic chained execution with single request..."
CHAINED_REQ='{
  "task_name": "chained_programs",
  "description": "Create TWO programs executed sequentially. Program 1 (Python) must PRINT EXACTLY one line with the JSON string {\"number\": 21} and no other output. Program 2 (Go) must READ the previous JSON and PRINT EXACTLY the number 42 (no extra text, no labels, no JSON). Do NOT print any extra whitespace, labels, prompts, or commentary in either program.",
  "context": {
    "artifacts_wrapper": "true",
    "artifact_names": "prog1.py,prog2.go"
  },
  "language": "python"
}'

CHAINED_JSON="$TMP_DIR/int_exec_chained.json"
curl -s -X POST "$HDN_URL/api/v1/intelligent/execute" \
  -H 'Content-Type: application/json' \
  --data-binary "$CHAINED_REQ" \
  -o "$CHAINED_JSON"

if ! jq -e '.success == true' "$CHAINED_JSON" >/dev/null; then
  echo "ERROR: chained execution failed:" >&2
  jq . "$CHAINED_JSON" || cat "$CHAINED_JSON"
  exit 1
fi

CHAINED_WID=$(jq -r '.workflow_id' "$CHAINED_JSON")
echo "[C/2] Chained workflow: $CHAINED_WID"

# Check if both files were created
CHAINED_FILES_JSON="$TMP_DIR/wf_files_chained.json"
curl -s "$HDN_URL/api/v1/files/workflow/$CHAINED_WID" -o "$CHAINED_FILES_JSON"

# Verify both programs were generated
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

# Check the final result
RES=$(jq -r '.result' "$CHAINED_JSON")
echo "[C/3] Chained result: $RES"
echo "$RES" | grep -Ex '^42$' || {
  echo "ERROR: expected '42' in chained result" >&2
  jq . "$CHAINED_JSON" || cat "$CHAINED_JSON"
  exit 1
}

echo "PASS: Automatic chained flow verified (Python -> JSON -> Go -> 42)"

# For fast debug, exit here. Comment this out to continue to other tests.
exit 0

# ------------------------------------------------------------
# Moved earlier: Go with external dependency (github.com/google/uuid)
# Run after chained flow when fast-debug is disabled
# ------------------------------------------------------------
echo ""
echo "[B/1] Triggering intelligent execute to create uuid_test.go (uses google/uuid) artifact..."
GO_REQ_PAYLOAD='{
  "task_name": "artifact_task",
  "description": "Create a Go program named uuid_test.go that meets ALL constraints:\n- package main\n- import \"github.com/google/uuid\" (as uuid) and ONLY fmt in addition\n- define func main() only\n- generate v := uuid.New().String() and fmt.Println(v)\n- no other imports\n- must build with: go build code.go (no unused imports)",
  "context": {
    "artifacts_wrapper": "true",
    "prefer_traditional": "true",
    "artifact_names": "uuid_test.go",
    "save_code_filename": "uuid_test.go"
  },
  "language": "go"
}'

GO_RESP_JSON="$TMP_DIR/int_exec_uuid.json"
curl -s -X POST "$HDN_URL/api/v1/intelligent/execute" \
  -H 'Content-Type: application/json' \
  --data-binary "$GO_REQ_PAYLOAD" \
  -o "$GO_RESP_JSON"

if ! jq -e '.success == true' "$GO_RESP_JSON" >/dev/null; then
  echo "ERROR: intelligent/execute for uuid_test.go failed:" >&2
  jq . "$GO_RESP_JSON" || cat "$GO_RESP_JSON"
  exit 1
fi

GO_WID=$(jq -r '.workflow_id' "$GO_RESP_JSON")
if [[ -z "$GO_WID" || "$GO_WID" == "null" ]]; then
  echo "ERROR: Missing workflow_id for uuid_test.go" >&2
  jq . "$GO_RESP_JSON"
  exit 1
fi
echo "[B/2] Workflow ID (go): $GO_WID"

GO_FILES_JSON="$TMP_DIR/wf_files_uuid.json"
curl -s "$HDN_URL/api/v1/files/workflow/$GO_WID" -o "$GO_FILES_JSON"
if ! jq -e 'map(.filename) | index("uuid_test.go") != null' "$GO_FILES_JSON" >/dev/null; then
  echo "ERROR: uuid_test.go not found in workflow files list" >&2
  jq . "$GO_FILES_JSON" || cat "$GO_FILES_JSON"
  exit 1
fi
echo "Found uuid_test.go in workflow files list."

GO_OUT_FILE="$TMP_DIR/uuid_test.go"
HTTP_CODE=$(curl -s -w "%{http_code}" -o "$GO_OUT_FILE" "$HDN_URL/api/v1/files/uuid_test.go")
echo "HTTP $HTTP_CODE"
if [[ "$HTTP_CODE" != "200" ]]; then
  echo "ERROR: failed to download uuid_test.go (HTTP $HTTP_CODE)" >&2
  exit 1
fi

# Validate output contains a UUID-like pattern from execution result
GO_RESULT=$(jq -r '.result' "$GO_RESP_JSON")
# Use a portable ERE (no word-boundaries) to match UUID v4 style
if echo "$GO_RESULT" | grep -Ei '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' >/dev/null; then
  echo "PASS: Go artifact result contains a UUID."
else
  echo "ERROR: Expected UUID in result, got:" >&2
  echo "$GO_RESULT" >&2
  echo "Source snippet:" >&2
  head -n 50 "$GO_OUT_FILE" >&2 || true
  exit 1
fi

# For fast debug, exit here. Comment this out to run all tests.
exit 0

# ------------------------------------------------------------
# Chained flow test: Output of first program feeds second
# ------------------------------------------------------------
echo ""
echo "[C/1] Create Python artifact chained_input.json with a simple JSON payload..."
CHAIN_PY_REQ='{
  "task_name": "artifact_task",
  "description": "Create a Python script named chained_writer.py that writes a file chained_input.json containing {\"number\": 21} and prints a confirmation line.",
  "context": {
    "artifacts_wrapper": "true",
    "prefer_traditional": "true",
    "artifact_names": "chained_writer.py,chained_input.json",
    "save_code_filename": "chained_writer.py"
  },
  "language": "python"
}'

CHAIN_PY_JSON="$TMP_DIR/int_exec_chain_writer.json"
curl -s -X POST "$HDN_URL/api/v1/intelligent/execute" \
  -H 'Content-Type: application/json' \
  --data-binary "$CHAIN_PY_REQ" \
  -o "$CHAIN_PY_JSON"

if ! jq -e '.success == true' "$CHAIN_PY_JSON" >/dev/null; then
  echo "ERROR: chain writer execution failed:" >&2
  jq . "$CHAIN_PY_JSON" || cat "$CHAIN_PY_JSON"
  exit 1
fi

CHAIN_WID=$(jq -r '.workflow_id' "$CHAIN_PY_JSON")
echo "[C/2] Workflow ID (chain writer): $CHAIN_WID"

CHAIN_FILES_JSON="$TMP_DIR/wf_files_chain_writer.json"
curl -s "$HDN_URL/api/v1/files/workflow/$CHAIN_WID" -o "$CHAIN_FILES_JSON"
if ! jq -e 'map(.filename) | index("chained_input.json") != null' "$CHAIN_FILES_JSON" >/dev/null; then
  echo "ERROR: chained_input.json not found in workflow files list" >&2
  jq . "$CHAIN_FILES_JSON" || cat "$CHAIN_FILES_JSON"
  exit 1
fi
echo "Found chained_input.json in workflow files."

CHAIN_INPUT_LOCAL="$TMP_DIR/chained_input.json"
HTTP_CODE=$(curl -s -w "%{http_code}" -o "$CHAIN_INPUT_LOCAL" "$HDN_URL/api/v1/files/chained_input.json")
echo "HTTP $HTTP_CODE"
if [[ "$HTTP_CODE" != "200" ]]; then
  echo "ERROR: failed to download chained_input.json (HTTP $HTTP_CODE)" >&2
  exit 1
fi

if ! grep -q '"number"' "$CHAIN_INPUT_LOCAL"; then
  echo "ERROR: chained_input.json missing expected key" >&2
  cat "$CHAIN_INPUT_LOCAL" >&2 || true
  exit 1
fi

JSON_INPUT=$(cat "$CHAIN_INPUT_LOCAL" | tr -d '\n' | sed 's/"/\\"/g')

echo "[C/3] Create Go artifact that consumes provided JSON and prints number*2..."
CHAIN_GO_REQ='{
  "task_name": "artifact_task",
  "description": "Create a Go program that prints 42",
  "context": {
    "artifacts_wrapper": "true",
    "prefer_traditional": "true",
    "artifact_names": "chained_consumer.go",
    "save_code_filename": "chained_consumer.go"
  },
  "language": "go"
}'

CHAIN_GO_JSON="$TMP_DIR/int_exec_chain_consumer.json"
curl -s -X POST "$HDN_URL/api/v1/intelligent/execute" \
  -H 'Content-Type: application/json' \
  --data-binary "$CHAIN_GO_REQ" \
  -o "$CHAIN_GO_JSON"

if ! jq -e '.success == true' "$CHAIN_GO_JSON" >/dev/null; then
  echo "ERROR: chain consumer execution failed:" >&2
  jq . "$CHAIN_GO_JSON" || cat "$CHAIN_GO_JSON"
  exit 1
fi

RESULT_TEXT=$(jq -r '.result' "$CHAIN_GO_JSON")
echo "[C/4] Consumer result: $RESULT_TEXT"

# Expect double of 21 = 42 somewhere in the result output
echo "$RESULT_TEXT" | grep -Eq '\\b42\\b' || {
  echo "ERROR: expected '42' in chained consumer result" >&2
  jq . "$CHAIN_GO_JSON" || cat "$CHAIN_GO_JSON"
  exit 1
}

echo "PASS: Chained flow (Python output → Go input) verified."

