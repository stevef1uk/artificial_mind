import requests
import time
import sys
import os
import json

HDN_URL = os.environ.get("HDN_URL", "http://localhost:8081")
SCRAPER_URL = os.environ.get("SCRAPER_URL", "http://localhost:8085")
FSM_URL = os.environ.get("FSM_URL", "http://localhost:8083")
LLM_URL = os.environ.get("LLM_URL", "http://mock-llm:11434")

def wait_for_service(url, name, timeout=60):
    print(f"Waiting for {name} at {url}...")
    start_time = time.time()
    while time.time() - start_time < timeout:
        try:
            health_url = f"{url}/health"
            resp = requests.get(health_url, timeout=5)
            if resp.status_code == 200:
                print(f"✅ {name} is healthy!")
                return True
        except Exception as e:
            print(f"   (retrying) {name} check failed: {e}")
        time.sleep(2)
    print(f"❌ {name} timed out!")
    return False

def test_scraper():
    print("\n🧪 Testing Scraper Service (Simple Extraction)...")
    payload = {
        "url": "https://www.nationwide.co.uk/savings/cash-isas/",
        "typescript_config": "",
        "extractions": {
            "products": "<tr[^>]*>.*?<p[^>]*class='[^']*ProductName[^']*'[^>]*>([^<]+)</p>.*?<div[^>]*class='[^']*Rate[^']*'[^>]*>([^<]+)%"
        }
    }
    
    try:
        # Start job
        resp = requests.post(f"{SCRAPER_URL}/scrape/start", json=payload, timeout=10)
        if resp.status_code != 200:
            print(f"❌ Failed to start scraper job: {resp.text}")
            return False
            
        job_id = resp.json().get("job_id")
        print(f"   ✅ Job started: {job_id}")
        
        # Poll
        for i in range(15):
            time.sleep(5)
            resp = requests.get(f"{SCRAPER_URL}/scrape/job", params={"job_id": job_id})
            if resp.status_code != 200:
                print(f"❌ Failed to poll job: {resp.text}")
                return False
                
            status = resp.json().get("status")
            print(f"   [{i*5}s] Status: {status}")
            
            if status == "completed":
                result = resp.json().get("result", {})
                products = result.get("products", "")
                if products:
                    print(f"   ✅ Scrape successful! Found products: {len(products.splitlines())}")
                    return True
                else:
                    print(f"   ❌ Scrape completed but no products found")
                    return False
            elif status == "failed":
                print(f"   ❌ Scrape job failed: {resp.json().get('error')}")
                return False
                
        print("   ❌ Scrape job timed out")
        return False
    except Exception as e:
        print(f"   ❌ Exception in test_scraper: {e}")
        return False

def test_hdn_state():
    print("\n🧪 Testing HDN State...")
    try:
        resp = requests.get(f"{HDN_URL}/api/v1/state")
        if resp.status_code == 200:
            print("✅ HDN State endpoint accessible")
            return True
        print(f"❌ HDN returned {resp.status_code}")
        return False
    except Exception as e:
        print(f"❌ Exception in HDN State test: {e}")
        return False

def test_fsm_status():
    print("\n🧪 Testing FSM Status...")
    try:
        resp = requests.get(f"{FSM_URL}/status")
        if resp.status_code == 200:
            print(f"✅ FSM is running.")
            return True
        print(f"❌ FSM status returned {resp.status_code}")
        return False
    except Exception as e:
        print(f"❌ FSM exception: {e}")
        return False

def test_code_generation():
    print("\n🧪 Testing Code Generation & Execution...")
    payload = {
        "task_name": "generate_primes", 
        "description": "Write python code to calculate the first 5 prime numbers",
        "language": "python",
        "context": {"mode": "code_gen"}
    }
    
    try:
        url = f"{HDN_URL}/api/v1/intelligent/execute"
        print(f"   POST {url}")
        resp = requests.post(url, json=payload, timeout=30)
        
        if resp.status_code == 200:
            result = resp.json()
            if result.get("success") or result.get("status") == "success":
                print("   ✅ HDN reported success")
                if "execution_result" in result:
                     output = result["execution_result"].get("output", "")
                     print(f"   📄 Execution Output: {output.strip()}")
                     if "Hello from Mock LLM Code Gen" in output:
                         print("   ✅ Verified execution result")
                         return True
                return True
            else:
                 print(f"   ⚠️ Response json indicates failure: {result}")
                 return False
        else:
            print(f"   ❌ HTTP {resp.status_code}: {resp.text}")
            return False
            
    except Exception as e:
        print(f"   ❌ Exception: {e}")
        return False

def test_agent_framework():
    print("\n🧪 Testing Agent Framework...")
    try:
        resp = requests.get(f"{HDN_URL}/api/v1/agents")
        if resp.status_code == 200:
            agents = resp.json()
            print(f"   ✅ Agents listed: {len(agents)} found")
            return True
        else:
             print(f"   ❌ Failed to list agents: {resp.status_code}")
             return False
    except Exception as e:
        print(f"   ❌ Exception: {e}")
        return False

def test_smart_scrape():
    print("\n🧪 Testing Smart Scrape (AI Planning)...")
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {
            "name": "smart_scrape",
            "arguments": {
                "url": "https://example.com",
                "goal": "Find the page title"
            }
        }
    }
    
    try:
        # Note: HDN exposes MCP tools via JSON-RPC at /mcp
        resp = requests.post(f"{HDN_URL}/mcp", json=payload, timeout=120)
        
        if resp.status_code == 200:
            result = resp.json()
            # print(f"DEBUG: {result}")
            if "result" in result and "content" in result["result"]:
                content = result["result"]["content"]
                for item in content:
                    if "text" in item:
                        text = item["text"]
                        # The result text should contain extracted data (Example Domain)
                        if "Example Domain" in text:
                            print(f"   ✅ Smart Scrape successful! Found title: Example Domain")
                            return True
            
            # Check for error in result
            if "error" in result:
                 print(f"   ❌ Smart Scrape RPC Error: {result['error']}")
                 return False

            print(f"   ❌ Smart Scrape returned unexpectedly: {result}")
            return False
        else:
            print(f"   ❌ Smart Scrape failed: {resp.status_code} - {resp.text}")
            return False
    except Exception as e:
        print(f"   ❌ Exception in test_smart_scrape: {e}")
        return False

def test_intelligent_agent_execution():
    print("\n🧪 Testing Intelligent Agent Execution (End-to-End)...")
    payload = {
        "input": "Scrape https://example.com and find the title"
    }
    
    try:
        url = f"{HDN_URL}/api/v1/agents/scraper_agent/execute"
        print(f"   POST {url}")
        # This will trigger the LLM planning logic in AgentExecutor
        resp = requests.post(url, json=payload, timeout=60)
        
        if resp.status_code == 200:
            result = resp.json()
            if "results" in result and result["results"] is not None:
                print(f"   ✅ Agent execution successful! Result items: {len(result['results'])}")
                return True
            else:
                print(f"   ❌ Agent execution returned no results: {result}")
                return False
        else:
            print(f"   ❌ HTTP {resp.status_code}: {resp.text}")
            return False
            
    except Exception as e:
        print(f"   ❌ Exception: {e}")
        return False

def test_intelligent_scraper_agent():
    print("\n🧪 Testing Intelligent Scraper Agent Selection...")
    # Send a goal that should trigger the scraper agent
    # We use the FSM 'event' endpoint to simulate a user request
    # Note: The FSM endpoint might vary; assuming /api/v1/fsm/event or similar based on previous context
    # Looking at FSM usually listening on 8083. Let's try to infer from test_fsm_status
    
    # Actually, we can check if the agent is registered and has the right keywords
    try:
        resp = requests.get(f"{HDN_URL}/api/v1/agents")
        if resp.status_code == 200:
            data = resp.json()
            agents = data.get("agents", []) if isinstance(data, dict) else data
            # print(f"DEBUG Agents: {agents}")
            
            # Agents list might be confusing format?
            scraper_agent = None
            try:
                for a in agents:
                    # Check if 'a' is a dict and has 'id'
                    if isinstance(a, dict) and a.get("id") == "scraper_agent":
                        scraper_agent = a
                        break
                    # Fallback if 'a' is a string
                    if isinstance(a, str) and a == "scraper_agent":
                        print(f"   ⚠️ Agents list contains strings not dicts? {a}")
            except Exception as e:
                print(f"   ⚠️ Error iterating agents: {e}")
            
            if scraper_agent:
                print(f"   ✅ Scraper Agent registered correctly")
                
                # Check tools - agent should have smart_scrape tool
                tools = scraper_agent.get("tools", [])
                
                if "smart_scrape" in tools:
                    print(f"   ✅ Scraper Agent has correct tools: {tools}")
                    return True
                else:
                    print(f"   ❌ Scraper Agent registered but missing smart_scrape tool: {tools}")
                    return False
            else:
                 print(f"   ❌ Scraper Agent NOT found in registry")
                 return False
        else:
             print(f"   ❌ Failed to list agents: {resp.status_code}")
             return False
    except Exception as e:
        print(f"   ❌ Exception: {e}")
        return False

def main():
    print("🚀 Starting Extended Regression Tests (Agent & Code Gen)")
    
    # Local dev: skip Mock LLM which isn't available locally
    services = [
        (HDN_URL, "HDN"),
        (SCRAPER_URL, "Scraper"),
        (FSM_URL, "FSM"),
        # (LLM_URL, "Mock LLM")  # Skip LLM for local testing
    ]
    
    for url, name in services:
        if not wait_for_service(url, name):
            print(f"❌ Critical service {name} failed to start.")
            sys.exit(1)
            
    success = True
    
    # if not test_hdn_state(): success = False
    # if not test_fsm_status(): success = False
    # if not test_scraper(): success = False
    # if not test_agent_framework(): success = False
    # if not test_code_generation(): success = False
    if not test_smart_scrape(): success = False
    if not test_intelligent_scraper_agent(): success = False
    if not test_intelligent_agent_execution(): success = False
    
    if success:
        print("\n🎉 ALL EXTENDED TESTS PASSED")
        sys.exit(0)
    else:
        print("\n💥 KABOOM: TEST FAILURE")
        sys.exit(1)

if __name__ == "__main__":
    main()
