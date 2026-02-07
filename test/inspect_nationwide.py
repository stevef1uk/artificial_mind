#!/usr/bin/env python3
"""
Helper script to:
1. Get the actual HTML from the scraper
2. Save it to a file
3. Show you what patterns to use for extraction
"""
import requests
import json
from datetime import datetime

HDN_URL = "http://localhost:8081"
SCRAPER_URL = "http://localhost:8085"

def inspect_page_content():
    """Get the raw page content and save it for inspection"""
    
    print("\n📋 Getting raw page content from Nationwide...")
    print(f"   Source: {SCRAPER_URL}")
    
    # Start a scrape job that returns the full page
    payload = {
        "url": "https://www.nationwide.co.uk/savings/compare-savings-accounts-and-isas/",
        "typescript_config": "await page.waitForLoadState('networkidle');",
        "extractions": {
            "full_text": ".*"  # Capture everything
        }
    }
    
    try:
        # Start job
        resp = requests.post(f"{SCRAPER_URL}/scrape/start", json=payload, timeout=10)
        if resp.status_code != 200:
            print(f"   ❌ Failed to start job: {resp.text}")
            return
        
        job_id = resp.json().get("job_id")
        print(f"   ✅ Job started: {job_id}")
        print(f"   ⏳ Waiting for page to render (30 seconds)...")
        
        # Poll for results
        import time
        for i in range(15):
            time.sleep(2)
            status_resp = requests.get(f"{SCRAPER_URL}/scrape/job", params={"job_id": job_id})
            status = status_resp.json().get("status")
            
            if status == "completed":
                result = status_resp.json()
                
                # Save HTML to file
                timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
                html_file = f"/tmp/nationwide_page_{timestamp}.html"
                
                # Try to save the page content
                print(f"\n   ✅ Job completed!")
                print(f"   📄 Result keys: {list(result.get('result', {}).keys())}")
                
                # Check what we got
                result_data = result.get("result", {})
                if result_data:
                    # Save to file
                    with open(html_file, "w") as f:
                        json.dump(result_data, f, indent=2)
                    
                    print(f"   💾 Saved to: {html_file}")
                    print(f"\n   📊 Data preview (first 1000 chars):")
                    
                    # Show what's in the response
                    for key, value in result_data.items():
                        if isinstance(value, str) and len(value) > 0:
                            preview = value[:200].replace("\n", " ")
                            print(f"      {key}: {preview}...")
                    
                    print(f"\n   💡 Next steps:")
                    print(f"      1. View the HTML: cat {html_file}")
                    print(f"      2. Search for product patterns (e.g., grep 'ISA' or 'aer')")
                    print(f"      3. Create regex patterns based on actual structure")
                
                return result_data
            
            print(f"   [{i*2}s] Status: {status}")
        
        print(f"   ⏱️ Timeout waiting for job")
        
    except Exception as e:
        print(f"   ❌ Error: {e}")

if __name__ == "__main__":
    inspect_page_content()
