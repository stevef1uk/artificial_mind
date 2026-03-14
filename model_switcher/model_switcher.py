#!/usr/bin/env python3
"""
Model Switcher for AX650
Manages switching between text LLM, vision LLM, and image generation based on requests.
Only one model can be loaded at a time due to TPU memory constraints.
"""
import subprocess
import time
import os
from flask import Flask, request, jsonify
import threading

app = Flask(__name__)

# Configuration
VISION_MODEL_DIR = "/home/stevef/dev/Qwen3-VL-2B-Instruct-axmodel"
IMAGE_GEN_MODEL_DIR = "/home/stevef/dev/lcm-lora-sdv1-5"

# Model port mapping
MODEL_PORTS = {
    'vision': 8001,
    'image_gen': 8806,
}

# Systemd service names
MODEL_SERVICES = {
    'vision': 'qwen3vl.service',
    'image_gen': 'lcm-serve.service',
}

# State
current_model = None  # 'text', 'vision', or 'image_gen'
model_lock = threading.Lock()


def kill_all_models():
    """Kill all running model processes"""
    global current_model

    print("[SWITCH] Killing all model processes...")

    # Stop all systemd services
    for name, service in MODEL_SERVICES.items():
        try:
            subprocess.run(['sudo', 'systemctl', 'stop', service],
                           capture_output=True, timeout=30)
            print(f"[SWITCH] Stopped {service}")
        except Exception as e:
            print(f"[SWITCH] Error stopping {service}: {e}")

    # Kill any stray processes
    for pattern in [
        'main_axcl_aarch64',
        'main_api_axcl',
        'tokenizer',
        'server/server.py',
        'server/main.py',
        'serve.py',          # LCM HTTP server
        'launcher.py',       # LCM launcher
    ]:
        subprocess.run(['sudo', 'pkill', '-f', pattern], capture_output=True)

    # Clean up temporary directories which might hold TPU locks
    for tmp in ['/tmp/axcl', '/tmp/ipc_ax*']:
        try:
            subprocess.run(['sudo', 'rm', '-rf', tmp], capture_output=True)
        except Exception:
            pass

    current_model = None

    # Wait for TPU memory to be fully released
    print("[SWITCH] Waiting for TPU memory to be released (30s)...")
    time.sleep(30)

    subprocess.run(['sync'], capture_output=True)
    print("[SWITCH] All models killed")


def _start_service(model_name: str, port: int, ready_wait: int = 5) -> bool:
    """Generic helper to start a systemd service and verify it becomes active."""
    service = MODEL_SERVICES[model_name]
    print(f"[SWITCH] Starting {model_name} model via {service}...")

    try:
        result = subprocess.run(
            ['sudo', 'systemctl', 'start', service],
            capture_output=True, text=True, timeout=10
        )
        if result.returncode != 0:
            print(f"[SWITCH] ✗ Failed to start {service}: {result.stderr}")
            return False

        print(f"[SWITCH] Service started, waiting {ready_wait}s for model to load...")
        time.sleep(ready_wait)

        check = subprocess.run(
            ['sudo', 'systemctl', 'is-active', service],
            capture_output=True, text=True
        )
        if check.stdout.strip() == 'active':
            print(f"[SWITCH] ✓ {model_name} model active on port {port}")
            return True
        else:
            print(f"[SWITCH] ✗ {service} not active after start")
            subprocess.run(['journalctl', '-u', service, '-n', '20', '--no-pager'])
            return False

    except Exception as e:
        print(f"[SWITCH] Error starting {model_name} model: {e}")
        return False



def start_vision_model() -> bool:
    global current_model
    # Vision model takes longer to load
    ok = _start_service('vision', MODEL_PORTS['vision'], ready_wait=5)
    if ok:
        current_model = 'vision'
    return ok


def start_image_gen_model() -> bool:
    global current_model
    # LCM serve takes ~30-60s to fully initialise on first request but the
    # service itself starts quickly; we use a short wait here and let clients
    # poll /health on the LCM service itself if they need to wait for readiness.
    ok = _start_service('image_gen', MODEL_PORTS['image_gen'], ready_wait=5)
    if ok:
        current_model = 'image_gen'
    return ok


# ── Routes ────────────────────────────────────────────────────────────────────

@app.route('/health', methods=['GET'])
def health():
    """Health check — returns switcher status and which model is active."""
    return jsonify({
        "status": "healthy",
        "current_model": current_model,
        "vision_running": current_model == 'vision',
        "image_gen_running": current_model == 'image_gen',
        "ports": {
            k: MODEL_PORTS[k] if current_model == k else None
            for k in MODEL_PORTS
        },
    })


@app.route('/current', methods=['GET'])
def get_current():
    """Return the currently active model and its port."""
    return jsonify({
        "current_model": current_model,
        "port": MODEL_PORTS.get(current_model),
    })


@app.route('/switch', methods=['POST'])
def switch_model():
    """
    Switch to a specific model.
    Body: {"model": "text" | "vision" | "image_gen"}
    """
    with model_lock:
        data = request.get_json(force=True) or {}
        target_model = data.get('model')

        valid_models = list(MODEL_SERVICES.keys())
        if target_model not in valid_models:
            return jsonify({
                "error": f"Invalid model '{target_model}'. Choose from: {valid_models}"
            }), 400

        if current_model == target_model:
            return jsonify({
                "message": f"{target_model} model is already running",
                "current_model": current_model,
                "port": MODEL_PORTS[current_model],
            })

        print(f"[SWITCH] Switching from {current_model!r} → {target_model!r}")
        kill_all_models()

        starters = {
            'vision': start_vision_model,
            'image_gen': start_image_gen_model,
        }
        success = starters[target_model]()

        if success:
            return jsonify({
                "message": f"Switched to {target_model} model",
                "current_model": current_model,
                "port": MODEL_PORTS.get(current_model),
            })
        else:
            return jsonify({
                "error": f"Failed to start {target_model} model",
                "current_model": current_model,
            }), 500


# ── Entry point ───────────────────────────────────────────────────────────────

def cleanup():
    print("[SWITCH] Shutting down...")
    kill_all_models()


if __name__ == '__main__':
    import atexit
    atexit.register(cleanup)

    print("=" * 70)
    print("AX650 Model Switcher")
    print("=" * 70)
    print("Endpoints:")
    print("  GET  /health   — switcher + model status")
    print("  GET  /current  — active model and port")
    print("  POST /switch   — body: {\"model\": \"vision\" | \"image_gen\"}")
    print("=" * 70)

    # Default model — override with AX650_DEFAULT_MODEL env var
    default_model = os.getenv('AX650_DEFAULT_MODEL', 'image_gen').lower()
    if default_model not in MODEL_SERVICES:
        print(f"[INIT] Unknown default model '{default_model}', falling back to 'image_gen'")
        default_model = 'image_gen'

    print(f"[INIT] Starting default model: {default_model}")
    starters = {
        'vision': start_vision_model,
        'image_gen': start_image_gen_model,
    }
    starters[default_model]()

    print("\nStarting API server on port 9000...")
    print("=" * 70)
    app.run(host='0.0.0.0', port=9000, debug=False, threaded=True)
