from flask import Flask, request, jsonify

app = Flask(__name__)

@app.route('/mcp', methods=['POST'])
def mcp_endpoint():
    data = request.json
    method = data.get('method')
    req_id = data.get('id')
    
    if method == 'tools/list':
        return jsonify({
            "jsonrpc": "2.0",
            "id": req_id,
            "result": {
                "tools": [
                    {
                        "name": "mock_tool_a",
                        "description": "A mock tool for testing",
                        "inputSchema": {
                            "type": "object",
                            "properties": {"arg1": {"type": "string"}},
                            "required": ["arg1"]
                        }
                    }
                ]
            }
        })
        
    elif method == 'tools/call':
        params = data.get('params', {})
        tool_name = params.get('name')
        args = params.get('arguments', {})
        
        return jsonify({
            "jsonrpc": "2.0",
            "id": req_id,
            "result": {
                "content": f"Executed {tool_name} with args: {args}",
                "success": True
            }
        })
        
    return jsonify({"jsonrpc": "2.0", "id": req_id, "error": {"code": -32601, "message": "Method not found"}})

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=3000)
