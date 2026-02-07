#!/usr/bin/env python3
"""
Test script to verify the Scraper Agent is functioning properly.
Tests direct scraper invocation and agent registration.
"""

import requests
import json
import time
import sys

HDN_URL = "http://localhost:8081"
SCRAPER_URL = "http://localhost:8085"

def test_scraper_agent_registered():
    """Check if scraper_agent is registered with correct tools."""
    print("\n🧪 Test 1: Scraper Agent Registration")
    print("=" * 60)
    
    try:
        resp = requests.get(f"{HDN_URL}/api/v1/agents", timeout=10)
        if resp.status_code != 200:
            print(f"❌ Failed to list agents: {resp.status_code}")
            return False
        
        agents = resp.json().get("agents", [])
        scraper_agent = None
        
        for agent in agents:
            if agent.get("id") == "scraper_agent":
                scraper_agent = agent
                break
        
        if not scraper_agent:
            print("❌ Scraper agent NOT found in registry")
            return False
        
        print(f"✅ Scraper agent found:")
        print(f"   Name: {scraper_agent.get('name')}")
        print(f"   Role: {scraper_agent.get('role')}")
        print(f"   Goal: {scraper_agent.get('goal')}")
        
        # Check tools
        tools = scraper_agent.get("tools", [])
        required_tools = ["smart_scrape", "scrape_url", "execute_code"]
        missing_tools = [t for t in required_tools if t not in tools]
        
        if missing_tools:
            print(f"❌ Missing tools: {missing_tools}")
            return False
        
        print(f"✅ All required tools present: {tools}")
        return True
        
    except Exception as e:
        print(f"❌ Exception: {e}")
        return False

def test_smart_scrape_mcp_tool():
    """Test that smart_scrape MCP tool works directly."""
    print("\n🧪 Test 2: Smart Scrape MCP Tool (Direct)")
    print("=" * 60)
    
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {
            "name": "smart_scrape",
            "arguments": {
                "url": "https://example.com",
                "goal": "Extract the page title"
            }
        }
    }
    
    try:
        print("Calling /mcp endpoint with smart_scrape tool...")
        resp = requests.post(f"{HDN_URL}/mcp", json=payload, timeout=60)
        
        if resp.status_code != 200:
            print(f"❌ HTTP Error {resp.status_code}: {resp.text}")
            return False
        
        result = resp.json()
        print(f"✅ Got response: {json.dumps(result, indent=2)[:500]}...")
        
        # Check for error in result
        if "error" in result:
            print(f"❌ JSON-RPC Error: {result['error']}")
            return False
        
        # Check for content
        if "result" in result and "content" in result["result"]:
            content = result["result"]["content"]
            if content and len(content) > 0:
                print(f"✅ Tool returned {len(content)} content items")
                for item in content:
                    if "text" in item:
                        text = item["text"]
                        if "Example Domain" in text:
                            print(f"✅ Found expected content: 'Example Domain'")
                            return True
        
        print(f"⚠️ Tool executed but no expected content in response")
        return False
        
    except Exception as e:
        print(f"❌ Exception: {e}")
        return False

def test_scraper_service_direct():
    """Test Playwright scraper service directly."""
    print("\n🧪 Test 3: Playwright Scraper Service (Direct)")
    print("=" * 60)
    
    # Health check
    try:
        print(f"Checking scraper health at {SCRAPER_URL}/health...")
        resp = requests.get(f"{SCRAPER_URL}/health", timeout=10)
        
        if resp.status_code != 200:
            print(f"❌ Scraper health check failed: {resp.status_code}")
            return False
        
        health = resp.json()
        print(f"✅ Scraper is healthy: {health}")
        
        # Try a scrape job
        print("\nSubmitting scrape job...")
        job_payload = {
            "url": "https://example.com",
            "typescript_config": "",
            "extractions": {
                "title": "<title>([^<]+)</title>"
            }
        }
        
        resp = requests.post(f"{SCRAPER_URL}/scrape/start", json=job_payload, timeout=10)
        if resp.status_code != 200:
            print(f"❌ Failed to start scrape job: {resp.status_code}")
            return False
        
        job_result = resp.json()
        job_id = job_result.get("job_id")
        print(f"✅ Job started: {job_id}")
        
        # Poll for result
        print("Waiting for job completion...")
        for i in range(30):  # 30 * 1 second = 30 seconds
            resp = requests.get(
                f"{SCRAPER_URL}/scrape/job",
                params={"job_id": job_id},
                timeout=10
            )
            
            if resp.status_code != 200:
                print(f"❌ Failed to poll job: {resp.status_code}")
                return False
            
            job_status = resp.json()
            status = job_status.get("status")
            
            if status == "completed":
                print(f"✅ Job completed!")
                result = job_status.get("result", {})
                print(f"   Result: {json.dumps(result, indent=2)}")
                return True
            elif status == "failed":
                print(f"❌ Job failed: {job_status.get('error')}")
                return False
            
            print(f"   [{i}s] Status: {status}")
            time.sleep(1)
        
        print("❌ Job timed out after 30 seconds")
        return False
        
    except Exception as e:
        print(f"❌ Exception: {e}")
        return False

def main():
    print("\n" + "=" * 60)
    print("🚀 SCRAPER AGENT VERIFICATION TEST SUITE")
    print("=" * 60)
    
    success = True
    
    # Run tests
    if not test_scraper_agent_registered(): success = False
    if not test_smart_scrape_mcp_tool(): success = False
    if not test_scraper_service_direct(): success = False
    
    # Summary
    print("\n" + "=" * 60)
    if success:
        print("✅ ALL TESTS PASSED - Scraper Agent is working properly!")
        print("=" * 60)
        return 0
    else:
        print("❌ SOME TESTS FAILED - See details above")
        print("=" * 60)
        return 1

if __name__ == "__main__":
    sys.exit(main())
