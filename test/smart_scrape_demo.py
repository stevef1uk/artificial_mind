import requests
import json
import time
import sys

# Configuration
HDN_URL = "http://localhost:8081"
GOAL = "Find the cheapest train ticket from Southampton to London" 
URL = "https://ecotree.green/en/calculate-train-co2" # Or whatever relevant URL

def run_smart_scrape(url, goal):
    print(f"🚀 Requesting Smart Scrape...")
    print(f"   URL: {url}")
    print(f"   Goal: {goal}")
    
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {
            "name": "smart_scrape",
            "arguments": {
                "url": url,
                "goal": goal
            }
        }
    }
    
    try:
        start_time = time.time()
        resp = requests.post(f"{HDN_URL}/mcp", json=payload, timeout=300) # Long timeout for LLM + Scrape
        duration = time.time() - start_time
        
        if resp.status_code == 200:
            result = resp.json()
            if "error" in result:
                print(f"❌ JSON-RPC Error: {result['error']}")
                return False
            
            print(f"✅ Success! (took {duration:.2f}s)")
            content = result.get("result", {}).get("content", [])
            for item in content:
                print("\n--- Result Content ---")
                print(item.get("text", ""))
                print("----------------------\n")
            return True
        else:
            print(f"❌ HTTP Error: {resp.status_code}")
            print(resp.text)
            return False
            
    except Exception as e:
        print(f"❌ Exception: {e}")
        return False

if __name__ == "__main__":
    if len(sys.argv) > 1:
        URL = sys.argv[1]
    if len(sys.argv) > 2:
        GOAL = sys.argv[2]
        
    success = run_smart_scrape(URL, GOAL)
    sys.exit(0 if success else 1)
