#!/usr/bin/env python3
"""
Test that actually extracts and returns Nationwide savings products and rates.

This uses TypeScript code to query the DOM for products and rates.
"""
import requests
import json
import sys
import os

HDN_URL = os.environ.get("HDN_URL", "http://localhost:8081")

def get_nationwide_products():
    """Extract Nationwide savings products using DOM queries via Playwright"""
    print("\n🧪 Extracting Nationwide Savings Products...")
    
    # Use TypeScript to query the page DOM for product data
    # This approach uses Playwright to find elements by selector and extract text
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {
            "name": "smart_scrape",
            "arguments": {
                "url": "https://www.nationwide.co.uk/savings/compare-savings-accounts-and-isas/",
                "goal": "Get all savings product names and their interest rates",
                # Use TypeScript to extract structured data via DOM selectors
                "typescript_config": """
// Wait for page to fully load
await page.waitForLoadState('networkidle');
await page.waitForTimeout(2000);

// Method 1: Try to get data from page text/content
const pageText = await page.innerText('body');

// Extract products and rates using regex from page content
const products = [];
const lines = pageText.split('\\n');

for (let i = 0; i < lines.length; i++) {
  const line = lines[i];
  // Look for patterns like "1 Year Fixed" or "ISA" with rate numbers
  if ((line.includes('Year') || line.includes('ISA') || line.includes('Saver') || line.includes('Bond')) && 
      !line.includes('Help') && line.trim().length > 3) {
    const nextLine = lines[i+1] || '';
    const rateLine = lines[i+2] || '';
    
    // Look for rate patterns (e.g., "3.8" or "3.8%")
    const rateMatch = rateLine.match(/[0-9]+\.?[0-9]*/);
    if (rateMatch) {
      products.push({
        name: line.trim().substring(0, 50),
        rate: rateMatch[0]
      });
    }
  }
}

return { products: products, total_lines: lines.length };
""",
                "extractions": {
                    "all_text": ".*"
                }
            }
        }
    }
    
    try:
        print(f"   POST {HDN_URL}/mcp")
        print(f"   Using Playwright TypeScript to extract product data...")
        
        resp = requests.post(f"{HDN_URL}/mcp", json=payload, timeout=60)
        
        if resp.status_code == 200:
            result = resp.json()
            
            if "error" in result:
                print(f"   ❌ Error: {result['error']}")
                return False
            
            if "result" in result:
                content = result["result"].get("content", [])
                if content:
                    for item in content:
                        if "text" in item:
                            text = item["text"]
                            print(f"\n   📄 Response received:")
                            print(f"   {text[:500]}")
                            
                            # Try to parse as JSON if it contains products
                            try:
                                # Look for JSON in the response
                                import re
                                json_match = re.search(r'\{[\s\S]*\}', text)
                                if json_match:
                                    data = json.loads(json_match.group(0))
                                    if "products" in data and data["products"]:
                                        print(f"\n   ✅ Found {len(data['products'])} products!")
                                        print(f"\n   📊 Savings Products & Rates:")
                                        for i, prod in enumerate(data["products"][:10]):
                                            if isinstance(prod, dict):
                                                print(f"      {i+1}. {prod.get('name', 'N/A')} - {prod.get('rate', 'N/A')}%")
                                            else:
                                                print(f"      {i+1}. {prod}")
                                        return True
                            except:
                                pass
                    
                    # If no structured data, at least show page was scraped
                    print(f"\n   ⚠️ Got response but couldn't extract structured products")
                    print(f"   Try inspecting the HTML to find the right selectors")
                    return False
        else:
            print(f"   ❌ HTTP {resp.status_code}: {resp.text}")
            return False
            
    except Exception as e:
        print(f"   ❌ Exception: {e}")
        import traceback
        traceback.print_exc()
        return False

if __name__ == "__main__":
    success = get_nationwide_products()
    
    if success:
        print("\n✅ Test PASSED - Products extracted!")
        sys.exit(0)
    else:
        print("\n❌ Test FAILED - Could not extract products")
        print("\n💡 Next steps:")
        print("   1. Inspect the actual HTML using curl/browser DevTools")
        print("   2. Find the CSS selectors for product names and rates")
        print("   3. Update the extraction regex or selectors")
        sys.exit(1)
