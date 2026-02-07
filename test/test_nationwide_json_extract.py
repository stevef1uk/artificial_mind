#!/usr/bin/env python3
"""
Test with better extraction patterns for Nationwide savings products.

Key insight: The page embeds product data in JSON within the HTML.
We need to extract from that structured data, not from the DOM.
"""
import requests
import json
import re
import sys
import os

HDN_URL = os.environ.get("HDN_URL", "http://localhost:8081")

def extract_nationwide_products():
    """
    Extract products using regex to find the JSON data embedded in the page.
    
    The Nationwide page contains product info in JSON like:
    {"name":"1 Year Fixed Rate Cash ISA","...":"aer":3.8,...}
    """
    print("\n🧪 Extracting Nationwide Savings Products (JSON method)...")
    
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {
            "name": "smart_scrape",
            "arguments": {
                "url": "https://www.nationwide.co.uk/savings/compare-savings-accounts-and-isas/",
                "goal": "Extract all savings products with their interest rates from the page",
                "typescript_config": "await page.waitForLoadState('networkidle');",
                "extractions": {
                    # Extract product names - matches JSON name fields
                    "product_names": '\"name\":\"([^\"]+)\"(?!.*category)',
                    
                    # Extract AER rates - matches "aer":3.8 patterns  
                    "interest_rates": '\"aer\":(\\d+(?:\\.\\d+)?)',
                    
                    # Extract product+rate pairs more carefully
                    "products_data": '\"name\":\"([^\"]+ISA|[^\"]+Bond|[^\"]+Saver)\"[^}]*\"aer\":(\\d+(?:\\.\\d+)?)'
                }
            }
        }
    }
    
    try:
        print(f"   Calling smart_scrape with extraction patterns...")
        print(f"   URL: Nationwide Savings Comparison")
        print(f"   Extracting: product names, interest rates, product data")
        
        resp = requests.post(f"{HDN_URL}/mcp", json=payload, timeout=60)
        
        if resp.status_code == 200:
            result = resp.json()
            
            if "error" in result:
                print(f"   ❌ Error: {result['error']['message']}")
                return False
            
            if "result" in result:
                # The result should have the extractions
                result_data = result["result"].get("result", {})
                
                print(f"\n   📊 Extracted Data:")
                print(f"   {json.dumps(result_data, indent=2)}")
                
                # Check for extracted products
                found_data = False
                
                if result_data.get("product_names"):
                    print(f"\n   ✅ Found product names:")
                    names = result_data["product_names"]
                    if isinstance(names, list):
                        for i, name in enumerate(names[:10]):
                            print(f"      {i+1}. {name}")
                        found_data = True
                
                if result_data.get("interest_rates"):
                    print(f"\n   ✅ Found interest rates:")
                    rates = result_data["interest_rates"]
                    if isinstance(rates, list):
                        for i, rate in enumerate(rates[:10]):
                            print(f"      {i+1}. {rate}%")
                        found_data = True
                
                return found_data
        else:
            print(f"   ❌ HTTP {resp.status_code}")
            return False
            
    except Exception as e:
        print(f"   ❌ Exception: {e}")
        return False

if __name__ == "__main__":
    success = extract_nationwide_products()
    
    if success:
        print("\n✅ Successfully extracted Nationwide products!")
        sys.exit(0)
    else:
        print("\n⚠️  Could not extract structured data")
        print("\nℹ️  The challenge:")
        print("   - Nationwide embeds product data in JSON within the HTML")
        print("   - Simple regex patterns need to account for JSON escaping")  
        print("   - The page uses JavaScript to render dynamic content")
        print("\n💡 To get the data, you need to:")
        print("   1. Get the actual rendered HTML from Playwright")
        print("   2. Find the exact structure (CSS selectors or JSON path)")
        print("   3. Create regex patterns that match that structure")
        sys.exit(1)
