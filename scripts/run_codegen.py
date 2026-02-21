#!/usr/bin/env python3
import sys
import json
import re
import os
import time

try:
    import requests
except ImportError:
    print("Error: 'requests' module not found. Please run: pip install requests")
    sys.exit(1)

def main():
    if len(sys.argv) < 2:
        print("Usage: python3 run_codegen.py <path_to_codegen_file.ts>")
        sys.exit(1)

    file_path = sys.argv[1]
    
    if not os.path.exists(file_path):
        print(f"Error: File '{file_path}' not found.")
        sys.exit(1)

    try:
        with open(file_path, 'r') as f:
            content = f.read()
    except Exception as e:
        print(f"Error reading file: {e}")
        sys.exit(1)

    # Extract the first URL to use as the base URL
    # Look for await page.goto('...') or "..."
    # We only care about the URL inside the first set of quotes
    url_match = re.search(r"await\s+page\.goto\(['\"]([^'\"]+)['\"]", content)
    url = "https://example.com" # Default fallback
    if url_match:
        url = url_match.group(1)
        print(f"Found URL in script: {url}")
    else:
        print(f"No page.goto found, using default: {url}")

    # Payload
    payload = {
        "url": url,
        "typescript_config": content,
        "get_html": True
    }

    # Using port 8085 as per service configuration
    api_url = "http://localhost:8085/scrape/start"
    print(f"Submitting job to {api_url}...")
    
    try:
        response = requests.post(api_url, json=payload)
        
        if response.status_code != 200:
            print(f"Error submitting job: {response.status_code} - {response.text}")
            sys.exit(1)
            
        result = response.json()
        job_id = result.get("job_id")
        
        if not job_id:
             print(f"Error: No job_id returned. Response: {result}")
             sys.exit(1)
             
        print(f"Job started! ID: {job_id}")
        
        # Poll for status
        print("Waiting for job completion...")
        while True:
            status_res = requests.get(f"http://localhost:8085/scrape/job?job_id={job_id}")
            if status_res.status_code != 200:
                print(f"Error checking status: {status_res.text}")
                break
                
            job_status = status_res.json()
            status = job_status.get("status")
            print(f"Status: {status}")
            
            if status in ["completed", "failed"]:
                print("-" * 40)
                if status == "completed":
                    print("Job Completed Successfully!")
                    # Check if there is data
                    res_data = job_status.get("result", {})
                    # Print result pretty
                    print(json.dumps(res_data, indent=2))
                else:
                    print("Job Failed.")
                    print(f"Error: {job_status.get('error')}")
                break
            
            time.sleep(1)
            
    except Exception as e:
        print(f"Error executing request: {e}")
        sys.exit(1)

if __name__ == "__main__":
    main()
