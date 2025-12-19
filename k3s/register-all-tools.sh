#!/bin/bash

# Register all default tools that should be available
# This registers tools that BootstrapSeedTools should have registered on startup

NAMESPACE="agi"
HDN_SERVICE="hdn-server-rpi58"

echo "🔧 Registering all default tools..."
echo

# Set up port-forward
kubectl port-forward -n $NAMESPACE svc/$HDN_SERVICE 8080:8080 >/tmp/hdn-tools-pf.log 2>&1 &
PF_PID=$!
sleep 3

if ! kill -0 $PF_PID 2>/dev/null; then
    echo "❌ Port-forward failed"
    exit 1
fi

echo "✅ Port-forward established (PID: $PF_PID)"
echo

# Register each tool
register_tool() {
    local tool_json="$1"
    local tool_id=$(echo "$tool_json" | jq -r '.id')
    
    response=$(curl -s -X POST http://localhost:8080/api/v1/tools \
        -H "Content-Type: application/json" \
        -d "$tool_json")
    
    if echo "$response" | jq -e '.success' >/dev/null 2>&1; then
        echo "✅ Registered: $tool_id"
        return 0
    else
        echo "⚠️  Failed to register $tool_id: $response"
        return 1
    fi
}

# List of all default tools (excluding the 3 already registered)
TOOLS=(
'{"id":"tool_html_scraper","name":"HTML Scraper","description":"Parse HTML and extract title/headings/paragraphs/links","input_schema":{"url":"string"},"output_schema":{"items":"array"},"permissions":["net:read"],"safety_level":"low","created_by":"system"}'
'{"id":"tool_file_read","name":"File Reader","description":"Read file","input_schema":{"path":"string"},"output_schema":{"content":"string"},"permissions":["fs:read"],"safety_level":"medium","created_by":"system"}'
'{"id":"tool_file_write","name":"File Writer","description":"Write file","input_schema":{"path":"string","content":"string"},"output_schema":{"written":"int"},"permissions":["fs:write"],"safety_level":"high","created_by":"system"}'
'{"id":"tool_ls","name":"List Directory","description":"List dir","input_schema":{"path":"string"},"output_schema":{"entries":"string[]"},"permissions":["fs:read"],"safety_level":"low","created_by":"system"}'
'{"id":"tool_exec","name":"Shell Exec","description":"Run shell command (sandboxed)","input_schema":{"cmd":"string"},"output_schema":{"stdout":"string","stderr":"string","exit_code":"int"},"permissions":["proc:exec"],"safety_level":"high","created_by":"system"}'
'{"id":"tool_docker_list","name":"Docker List","description":"List docker entities","input_schema":{"type":"string"},"output_schema":{"items":"string[]"},"permissions":["docker"],"safety_level":"medium","created_by":"system"}'
'{"id":"tool_codegen","name":"Codegen","description":"Generate code via LLM","input_schema":{"spec":"string"},"output_schema":{"code":"string"},"permissions":["llm"],"safety_level":"medium","created_by":"system"}'
'{"id":"tool_docker_build","name":"Docker Build","description":"Build tool image","input_schema":{"path":"string"},"output_schema":{"image":"string"},"permissions":["docker"],"safety_level":"medium","created_by":"system"}'
'{"id":"tool_register","name":"Register Tool","description":"Register tool metadata","input_schema":{"tool":"json"},"output_schema":{"ok":"bool"},"permissions":["registry:write"],"safety_level":"low","created_by":"system"}'
'{"id":"tool_json_parse","name":"JSON Parse","description":"Parse JSON","input_schema":{"text":"string"},"output_schema":{"object":"json"},"permissions":[],"safety_level":"low","created_by":"system"}'
'{"id":"tool_text_search","name":"Text Search","description":"Search text","input_schema":{"pattern":"string","text":"string"},"output_schema":{"matches":"string[]"},"permissions":[],"safety_level":"low","created_by":"system"}'
)

REGISTERED=0
FAILED=0

for tool_json in "${TOOLS[@]}"; do
    if register_tool "$tool_json"; then
        REGISTERED=$((REGISTERED + 1))
    else
        FAILED=$((FAILED + 1))
    fi
    sleep 0.5  # Small delay to avoid overwhelming the API
done

# Cleanup
kill $PF_PID 2>/dev/null
wait $PF_PID 2>/dev/null

echo
echo "============================"
echo "✅ Registered: $REGISTERED tools"
if [ $FAILED -gt 0 ]; then
    echo "⚠️  Failed: $FAILED tools"
fi

# Verify
echo
echo "Verifying tools in Redis..."
TOTAL_TOOLS=$(kubectl exec -n $NAMESPACE deployment/redis -- redis-cli SMEMBERS tools:registry 2>/dev/null | wc -l)
echo "Total tools in registry: $TOTAL_TOOLS"

echo
echo "✅ Complete! Tools should now be visible in Monitor UI"

