from flask import Flask, request, jsonify
import time
import os

app = Flask(__name__)

def get_smart_response(msg):
    lower_msg = msg.lower()
    if "python" in lower_msg or "code" in lower_msg or "calculate" in lower_msg:
        return """Here is the Python code:
```python
print('Hello from Mock LLM Code Gen')
x = 10 + 20
print(f'Result: {x}')
```"""
    elif "agent" in lower_msg or "plan" in lower_msg:
        return "Plan: 1. Step A, 2. Step B. Confirmed."
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

    print(f"🦙 [Mock Ollama] Received request: {last_msg[:50]}...")

    content = f"Mock Ollama response to: {last_msg[:20]}..."
    
    # Check for keywords to trigger specific behaviors
    lower_msg = last_msg.lower()
    
    # Behavior 1: Code Generation
    if "python" in lower_msg or "code" in lower_msg or "calculate" in lower_msg:
        content = """Here is the Python code you requested:
```python
print('Hello from Mock LLM Code Gen')
x = 10 + 20
print(f'Result: {x}')
```"""
    
    # Behavior 2: Agent Planning / Thought Process
    elif "agent" in lower_msg or "plan" in lower_msg or "task" in lower_msg:
        content = """I have analyzed the task. Here is the plan:
1. Identify the goal.
2. Execute the necessary steps.
3. Verify the result.
Confirmed."""

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

if __name__ == '__main__':
    port = int(os.environ.get('PORT', 11434))
    app.run(host='0.0.0.0', port=port)
