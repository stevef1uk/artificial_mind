import requests
import json
import time

url = "http://localhost:8081/api/v1/chat"

def send_chat(session_id, message):
    payload = {
        "message": message,
        "session_id": session_id,
        "context": {}
    }
    print(f"\n--- Sending Request to {url} ---")
    print(f"Session ID: {session_id}")
    print(f"Message: {message}")
    try:
        response = requests.post(url, json=payload, timeout=300)
        print(f"Status Code: {response.status_code}")
        print(f"Duration: {response.elapsed.total_seconds():.2f}s")
        
        if response.status_code == 200:
            data = response.json()
            print(f"AI Response: {data.get('response', '')}")
            print(f"Action Type: {data.get('action', '')}")
            # print(f"Metadata: {data.get('metadata', {})}")
            return data
        else:
            print(f"Error: {response.text}")
            return None
    except Exception as e:
        print(f"Error: {e}")
        return None

def main():
    session_id = f"test_chat_session_{int(time.time())}"
    
    # Step 1: Initial image
    print(f"Step 1: Creating initial image of a dog...")
    original_prompt = "Use the image generation tool to create an image of a dog."
    res1 = send_chat(session_id, original_prompt)

    if not res1 or res1.get("action") not in ["conversation_result", "task_result"]:
        print("❌ Step 1 failed to trigger tool_generate_image")
    else:
        print("✅ Step 1 triggered tool_generate_image")

    # Wait a bit
    print("\n--- Waiting 3 seconds before modification ---")
    time.sleep(3)

    # Step 2: Modification
    update_request = "Change the background of the dog image to yellow, keeping the dog unchanged."
    combined_prompt = update_request
    print(f"Step 2: Requesting modification (yellow background, keep dog unchanged)...")
    res2 = send_chat(session_id, combined_prompt)

    if not res2 or res2.get("action") not in ["conversation_result", "task_result"]:
        print("❌ Step 2 failed to trigger tool_generate_image")
    else:
        print("✅ Step 2 triggered tool_generate_image")

if __name__ == "__main__":
    main()
