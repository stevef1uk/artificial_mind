#!/usr/bin/env python3
"""Simple test to check LLM proxy code generation - mimics HDN requests"""

import requests
import json

# Config from your llm-config-secret.yaml
BASE_URL = "http://192.168.1.60:11434"
MODEL = "qwen2.5-1.5b-instruct"

# Code generation prompt (similar to HDN's IntelligentExecutor)
PROMPT = """You are an expert programmer. Write a Python function that:
- Calculates factorial using recursion
- Function name: factorial
- Has error handling for negative numbers
- Includes type hints

Respond with ONLY the code, no explanations."""

def test_endpoint(url, payload):
    """Test a specific endpoint"""
    print(f"\n{'='*60}")
    print(f"Testing: {url}")
    print(f"{'='*60}")
    try:
        resp = requests.post(url, json=payload, timeout=60)
        print(f"Status: {resp.status_code}")
        if resp.status_code == 200:
            data = resp.json()
            print(f"✅ SUCCESS!")
            print(json.dumps(data, indent=2))
            return True
        else:
            print(f"❌ Error: {resp.text[:200]}")
    except Exception as e:
        print(f"❌ Exception: {e}")
    return False

# Test 1: Ollama /api/generate format (used by wiki-summarizer)
print("\n🧪 Test 1: /api/generate endpoint")
payload1 = {
    "model": MODEL,
    "prompt": PROMPT,
    "stream": False
}
test_endpoint(f"{BASE_URL}/api/generate", payload1)

# Test 2: Ollama /api/chat format (used by HDN)
print("\n🧪 Test 2: /api/chat endpoint")
payload2 = {
    "model": MODEL,
    "messages": [{"role": "user", "content": PROMPT}],
    "stream": False
}
test_endpoint(f"{BASE_URL}/api/chat", payload2)

# Test 3: OpenAI-compatible format
print("\n🧪 Test 3: /v1/chat/completions endpoint")
payload3 = {
    "model": MODEL,
    "messages": [{"role": "user", "content": PROMPT}],
    "temperature": 0.7,
    "max_tokens": 1000
}
test_endpoint(f"{BASE_URL}/v1/chat/completions", payload3)

print("\n" + "="*60)
print("Tests complete!")
