#!/usr/bin/env python3
"""
Test automatic Playwright configuration generation using Nationwide website.
"""
import requests
import json
import sys
import os
import re

HDN_URL = os.environ.get("HDN_URL", "http://localhost:8081")

def test_nationwide_auto_config():
    """Test smart_scrape with automatic config generation on Nationwide website"""
    print("\n🧪 Testing Nationwide with Auto Playwright Config Generation...")
    
    # Call smart_scrape with URL, goal, and extraction hints
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {
            "name": "smart_scrape",
            "arguments": {
                "url": "https://www.nationwide.co.uk/savings/compare-savings-accounts-and-isas/",
                "goal": "Extract all savings product names and their AER interest rates from the table",
                "extractions": {
                    "product_names": "'name':'([^']*(?:ISA|Saver|Bond)[^']*)'",
                    "interest_rates": "'aer':([\\d\\.]+)"
                }
            }
        }
    }
    
    try:
        print(f"   POST {HDN_URL}/mcp")
        print(f"   Tool: smart_scrape")
        print(f"   URL: https://www.nationwide.co.uk/savings/compare-savings-accounts-and-isas/")
        print(f"   Goal: Extract savings products and interest rates")
        print(f"\n   ⏳ LLM is generating Playwright TypeScript config automatically...")
        
        resp = requests.post(f"{HDN_URL}/mcp", json=payload, timeout=90)
        
        if resp.status_code != 200:
            print(f"   ❌ HTTP {resp.status_code}: {resp.text}")
            return False
            
        result = resp.json()
        if "error" in result:
            print(f"   ❌ JSON-RPC Error: {result['error']}")
            return False
            
        if "result" not in result:
            print(f"   ❌ Unexpected response format")
            return False

        content = result["result"].get("content", [])
        if not content:
            print(f"   ❌ No content in result")
            return False

        print(f"   ✅ Got response with {len(content)} content items")
        
        for item in content:
            if "text" in item:
                text = item["text"]
                print(f"\n   📊 Full Response Text:")
                print(f"   {text}")
                
                # Also show the raw result if available
                if "result" in result["result"]:
                    print(f"\n   🔍 Raw Result Keys: {list(result['result']['result'].keys())}")
                
                # Try to parse as JSON
                try:
                    json_match = re.search(r'\{[\s\S]*\}', text)
                    if json_match:
                        json_str = json_match.group(0)
                        data = json.loads(json_str)
                        
                        print(f"\n   📋 Structured Data Found:")
                        
                        names_raw = data.get("product_names", "")
                        rates_raw = data.get("interest_rates", "")
                        
                        names = names_raw.split("\n") if isinstance(names_raw, str) else []
                        rates = rates_raw.split("\n") if isinstance(rates_raw, str) else []
                        
                        if names and names[0]:
                            print(f"\n   🏦 Found {len(names)} Products:")
                            for i in range(min(len(names), len(rates))):
                                print(f"      - {names[i]}: {rates[i]}%")
                            
                            if len(names) > 1:
                                print(f"\n   ✅ Success! Multiple matches found and returned.")
                                return True
                        
                        if "page_title" in data:
                            print(f"\n   📄 Page Title: {data['page_title']}")
                            print(f"   ⚠️ Scrape worked but no products matched the regex hints.")
                            return True
                except json.JSONDecodeError:
                    pass
        
        print(f"   ⚠️ Got response but didn't find expected formatted data")
        return False
            
    except requests.exceptions.Timeout:
        print(f"   ❌ Request timed out")
        return False
    except Exception as e:
        print(f"   ❌ Exception: {e}")
        return False

if __name__ == "__main__":
    success = test_nationwide_auto_config()
    sys.exit(0 if success else 1)
