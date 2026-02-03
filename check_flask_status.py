#!/usr/bin/env python3
"""Check Flask LLM server status and queue"""
import requests

FLASK_URL = "http://192.168.1.60:8000"

print("🔍 Checking Flask LLM Server Status")
print("="*60)

# Try to get server info
try:
    # Check if server is alive
    resp = requests.get(f"{FLASK_URL}/health", timeout=5)
    print(f"✅ Server is responding: {resp.status_code}")
except:
    print("❌ Server not responding on /health")

# Try to get any status endpoint
try:
    resp = requests.get(f"{FLASK_URL}/status", timeout=5)
    if resp.status_code == 200:
        print(f"Status: {resp.json()}")
except:
    print("⚠️  No /status endpoint")

# Try to reset/clear queue if endpoint exists
try:
    resp = requests.post(f"{FLASK_URL}/api/reset", json={}, timeout=5)
    print(f"Reset response: {resp.status_code}")
except:
    print("⚠️  No /api/reset endpoint")

print("\n💡 Recommendations:")
print("1. Restart the Flask server to clear any queued requests")
print("2. Make sure only ONE request is sent at a time")
print("3. The TPU can take 60-120 seconds per request")
