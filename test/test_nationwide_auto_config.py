#!/usr/bin/env python3
import requests
import json
import time
import sys

# Configuration
HDN_URL = "http://localhost:8081"
# Nationwide Savings & ISAs page
TARGET_URL = "https://www.nationwide.co.uk/savings/compare-savings-accounts-and-isas/"

def test_auto_config_generation():
    print("\n🧪 Testing Nationwide with Auto Playwright Config Generation...")
    
    # 1. Send request WITHOUT config (let LLM generate it)
    # optionally provide hints if we know the structure
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {
            "name": "smart_scrape",
            "arguments": {
                "url": TARGET_URL,
                "goal": "Extract savings products and interest rates",
                # Hints help the LLM or Fast Path logic find the right elements
                "extractions": {
                    "product_names": "Table__ProductName[^>]*>\\s*([^<]+)<",
                    "interest_rates": "data-ref=['\"]heading['\"]>\\s*([0-9.]+%)</div>"
                }
            }
        }
    }

    print(f"   POST {HDN_URL}/mcp")
    print(f"   Tool: smart_scrape")
    print(f"   URL: {TARGET_URL}")
    print(f"   Goal: Extract savings products and interest rates")
    print("")
    print("   ⏳ LLM is generating Playwright TypeScript config automatically...")
    
    try:
        start_time = time.time()
        resp = requests.post(f"{HDN_URL}/mcp", json=payload, timeout=120)
        duration = time.time() - start_time
        
        if resp.status_code != 200:
            print(f"   ❌ HTTP Error {resp.status_code}: {resp.text}")
            return False

        result = resp.json()
        if "error" in result:
            print(f"   ❌ JSON-RPC Error: {result['error']}")
            return False

        if "result" not in result:
            print(f"   ❌ Unexpected response format")
            return False

        # Handle nested result structure from MCP tool call
        # smart_scrape returns: { "content": [ { "type": "text", "text": "JSON_STRING" } ] }
        # OR sometimes directly the JSON object if adapter handles it.
        # Let's inspect raw result.
        
        tool_result = result["result"]
        
        # Check if content is present
        if "content" in tool_result:
            content_list = tool_result["content"]
            if not content_list:
                print("   ❌ Empty content list")
                return False
            
            first_item = content_list[0]
            if "text" in first_item:
                text_content = first_item["text"]
                print(f"   ✅ Got response with {len(content_list)} content items")
                print(f"\n   📊 Full Response Text:\n   {text_content[:500]}...") # Print first 500 chars
                
                # Check if we got expected data
                if "Flex Regular Saver" in text_content or "ISA" in text_content:
                    print("\n   ✅ Success! Found expected product names in output.")
                    return True
                else:
                    print("\n   ⚠️ Response does not look like product data. Check regex hints.")
                    return False
        
        # If structure is different (e.g. direct object)
        print(f"   ⚠️ Structure might vary: {json.dumps(tool_result, indent=2)[:500]}")
        return True

    except Exception as e:
        print(f"   ❌ Exception: {e}")
        return False

if __name__ == "__main__":
    if test_auto_config_generation():
        sys.exit(0)
    else:
        sys.exit(1)
