from flask import Flask, request, jsonify
import time
import os

app = Flask(__name__)

def get_smart_response(msg):
    lower_msg = msg.lower()
    
    # 1. SPECIFIC SUCCESS CASES (Highest Priority)
    # This must come first to avoid being caught by generic keyword matches below
    if ("example" in lower_msg and "domain" in lower_msg) and ("title" in lower_msg or "find" in lower_msg):
        return "Example Domain"
        
    # 2. HYPOTHESIS GENERATION / AGENT PLANNING
    # Prompts that ask for 1 to 3 experiment ideas
    if "generate 1 to 3" in lower_msg or "experiment ideas" in lower_msg or "json array of tool calls" in lower_msg or "scraper_agent" in lower_msg:
        return """[
  {
    "description": "If we scrape the example.com domain, we will find the title is Example Domain.",
    "confidence": 0.9
  }
]"""
    
    # 3. CODE GENERATION
    # Only trigger if specifically asked for code or calculations
    if ("write" in lower_msg or "generate" in lower_msg) and ("python code" in lower_msg or "calculate the" in lower_msg):
        return """Here is the Python code:
```python
print('Hello from Mock LLM Code Gen')
x = 10 + 20
print(f'Result: {x}')
```"""
    
    # 4. SCRAPER CONFIGURATION PLANNING
    # Only if asked to plan or configure a scraper
    if ("plan" in lower_msg or "configure" in lower_msg) and ("scraper" in lower_msg or "scraping" in lower_msg):
        return """```json
{
  "typescript_config": "",
  "extractions": {
    "title": "<h1>(.*?)</h1>"
  }
}
```"""
    
    return f"Mock response to: {msg[:20]}... [Processed by Mock LLM]"


@app.route('/health', methods=['GET'])
def health():
    return jsonify({"status": "healthy"})

@app.route('/goals/<agent_id>/active', methods=['GET'])
def get_active_goals(agent_id):
    return jsonify([])

# OpenAI Compatible Endpoint
@app.route('/v1/chat/completions', methods=['POST'])
def chat_completions():
    data = request.json
    messages = data.get('messages', [])
    last_msg = messages[-1]['content'] if messages else ""
    
    print(f"🤖 [Mock LLM] Received request: {last_msg[:50]}...")
    
    # Mock response
    return jsonify({
        "id": "chatcmpl-mock",
        "object": "chat.completion",
        "created": int(time.time()),
        "choices": [{
            "index": 0,
            "message": {
                "role": "assistant",
                "content": get_smart_response(last_msg)
            },
            "finish_reason": "stop"
        }],
        "usage": {
            "prompt_tokens": 10,
            "completion_tokens": 10,
            "total_tokens": 20
        }
    })

# Ollama Compatible Endpoint
@app.route('/api/chat', methods=['POST'])
def ollama_chat():
    data = request.json
    messages = data.get('messages', [])
    last_msg = messages[-1]['content'] if messages else ""

    # Check for keywords to trigger specific behaviors
    lower_msg = last_msg.lower()
    
    content = f"Mock Ollama response to: {last_msg[:20]}..."
    
    # 1. SPECIFIC SUCCESS CASES (Highest Priority)
    # Ensure this matches the smart_scrape prompt for example.com
    if ("example" in lower_msg and "domain" in lower_msg) and ("title" in lower_msg or "find" in lower_msg):
        content = "Example Domain"
    
    # 2. HYPOTHESIS GENERATION / AGENT PLANNING
    elif "generate 1 to 3" in lower_msg or "experiment ideas" in lower_msg or "json array of tool calls" in lower_msg or "scraper_agent" in lower_msg or ("plan" in lower_msg and "experiment" in lower_msg):
        content = """[
  {
    "description": "If we scrape the example.com domain, we will find the title is Example Domain.",
    "confidence": 0.9
  }
]"""
    
    # 3. CODE GENERATION
    elif ("write" in lower_msg or "generate" in lower_msg) and ("python code" in lower_msg or "calculate the" in lower_msg):
        content = """Here is the Python code you requested:
```python
print('Hello from Mock LLM Code Gen')
x = 10 + 20
print(f'Result: {x}')
```"""

    # 4. SCRAPER CONFIGURATION PLANNING
    elif ("plan" in lower_msg or "configure" in lower_msg) and ("scraper" in lower_msg or "scraping" in lower_msg):
        content = """```json
{
  "typescript_config": "",
  "extractions": {
    "title": "<h1>(.*?)</h1>"
  }
}
```"""



    # Behavior 4: Intent Classification
    elif "classify" in lower_msg and "category" in lower_msg:
        if "summarize" in lower_msg or "news" in lower_msg or "iran" in lower_msg:
            content = "query"
        elif "remember" in lower_msg or "my name is" in lower_msg:
            content = "personal_update"
        else:
            content = "general_conversation"

    # Behavior 5: Entity Extraction
    elif "extract entities" in lower_msg or "return as json" in lower_msg:
        if "iran" in lower_msg:
            content = '{"query": "iran", "topic": "news"}'
        elif "remember" in lower_msg:
            content = '{"content": "remember that I like coffee"}'
        else:
            content = '{}'

    return jsonify({
        "model": data.get("model", "mock-model"),
        "created_at": "2023-01-01T00:00:00Z",
        "message": {
            "role": "assistant",
            "content": content
        },
        "done": True,
        "total_duration": 100,
        "load_duration": 10,
        "prompt_eval_count": 10,
        "eval_count": 10
    })

@app.route('/api/generate', methods=['POST'])
def ollama_generate():
    data = request.json
    prompt = data.get('prompt', '').lower()

    print(f"🦙 [Mock Ollama Generate] Received request: {prompt[:50]}...")

    # Default flight response
    content = """{
  "departure": "LHR",
  "destination": "JFK",
  "start_date": "2026-05-01",
  "end_date": "2026-05-10",
  "cabin": "Economy"
}"""

    # If it's a Miner request (HTML extraction)
    if "extract all flight options" in prompt or "airline" in prompt:
        content = """[
  {
    "airline": "British Airways",
    "departure_time": "10:00 AM",
    "arrival_time": "1:00 PM",
    "duration": "7h 0m",
    "stops": "Nonstop",
    "price": "£450"
  },
  {
    "airline": "Virgin Atlantic",
    "departure_time": "12:00 PM",
    "arrival_time": "3:00 PM",
    "duration": "7h 0m",
    "stops": "Nonstop",
    "price": "£480"
  }
]"""

    return jsonify({
        "model": data.get("model", "mock-model"),
        "created_at": "2023-01-01T00:00:00Z",
        "response": content,
        "done": True
    })

@app.route('/api/embeddings', methods=['POST'])
def ollama_embeddings():
    return jsonify({
        "embedding": [0.1] * 1024
    })

if __name__ == '__main__':
    port = int(os.environ.get('PORT', 11434))
    app.run(host='0.0.0.0', port=port)
