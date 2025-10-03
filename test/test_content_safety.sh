#!/bin/bash

# Test script for content safety system
echo "🛡️ Testing Content Safety System"
echo "================================="

# Test URLs
echo ""
echo "1. Testing safe URLs (should work):"
echo "-----------------------------------"

# Safe URLs
safe_urls=(
    "https://httpbin.org/get"
    "https://www.wikipedia.org"
    "https://github.com"
    "https://stackoverflow.com"
    "https://developer.mozilla.org"
)

for url in "${safe_urls[@]}"; do
    echo "Testing: $url"
    response=$(curl -s -X POST http://localhost:8081/api/v1/tools/tool_http_get/invoke \
        -H "Content-Type: application/json" \
        -d "{\"url\": \"$url\"}")
    
    if echo "$response" | grep -q '"status":200'; then
        echo "✅ ALLOWED"
    elif echo "$response" | grep -q "content blocked"; then
        echo "❌ BLOCKED: $(echo "$response" | jq -r '.reason // "unknown reason"')"
    else
        echo "⚠️ ERROR: $response"
    fi
    echo ""
done

echo ""
echo "2. Testing blocked URLs (should be blocked):"
echo "--------------------------------------------"

# Blocked URLs (these should be blocked)
blocked_urls=(
    "https://pornhub.com"
    "https://xvideos.com"
    "https://malware.com"
    "https://phishing.com"
    "https://bitcoin-scam.com"
)

for url in "${blocked_urls[@]}"; do
    echo "Testing: $url"
    response=$(curl -s -X POST http://localhost:8081/api/v1/tools/tool_http_get/invoke \
        -H "Content-Type: application/json" \
        -d "{\"url\": \"$url\"}")
    
    if echo "$response" | grep -q "content blocked"; then
        echo "✅ BLOCKED: $(echo "$response" | jq -r '.reason // "unknown reason"')"
    elif echo "$response" | grep -q '"status":200'; then
        echo "❌ ALLOWED (should be blocked!)"
    else
        echo "⚠️ ERROR: $response"
    fi
    echo ""
done

echo ""
echo "3. Testing suspicious URLs (should be blocked):"
echo "-----------------------------------------------"

# Suspicious URLs
suspicious_urls=(
    "https://bit.ly/suspicious"
    "https://192.168.1.1/malicious"
    "https://suspicious.tk"
    "https://malicious.ml"
)

for url in "${suspicious_urls[@]}"; do
    echo "Testing: $url"
    response=$(curl -s -X POST http://localhost:8081/api/v1/tools/tool_http_get/invoke \
        -H "Content-Type: application/json" \
        -d "{\"url\": \"$url\"}")
    
    if echo "$response" | grep -q "content blocked"; then
        echo "✅ BLOCKED: $(echo "$response" | jq -r '.reason // "unknown reason"')"
    elif echo "$response" | grep -q '"status":200'; then
        echo "❌ ALLOWED (should be blocked!)"
    else
        echo "⚠️ ERROR: $response"
    fi
    echo ""
done

echo ""
echo "4. Checking tool metrics for blocked requests:"
echo "----------------------------------------------"

metrics=$(curl -s http://localhost:8081/api/v1/tools/metrics)
echo "Tool metrics:"
echo "$metrics" | jq '.'

echo ""
echo "5. Checking recent tool calls:"
echo "-------------------------------"

recent=$(curl -s http://localhost:8081/api/v1/tools/calls/recent)
echo "Recent calls:"
echo "$recent" | jq '.calls[] | {tool_id, status, error, parameters}'

echo ""
echo "🎉 Content Safety Test Complete!"
echo ""
echo "Summary:"
echo "- Safe URLs should be ALLOWED"
echo "- Blocked URLs should be BLOCKED"
echo "- Suspicious URLs should be BLOCKED"
echo "- All blocked requests should be logged in metrics"
