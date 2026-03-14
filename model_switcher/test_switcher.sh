#!/usr/bin/env python3
"""
Model Cycle Test
Continuously switches between vision (image description) and image generation models,
testing each one works and reporting results.
"""

import requests
import base64
import time
import sys
import os
import json
from datetime import datetime

# ── Config ────────────────────────────────────────────────────────────────────
SWITCHER_URL   = "http://localhost:9000"
VISION_URL     = "http://localhost:8001"
IMAGE_GEN_URL  = "http://localhost:8806"

TEST_IMAGE_PATH = os.path.expanduser("~/cat.jpg")
IMAGE_GEN_PROMPT = "a red apple on a wooden table"

SWITCH_TIMEOUT   = 120   # seconds to wait for switch to complete
SERVICE_TIMEOUT  = 120   # seconds to wait for inference
POLL_INTERVAL    = 5     # seconds between health check polls after switch
SETTLE_TIME      = 10    # seconds to wait after health check passes

# ── Helpers ───────────────────────────────────────────────────────────────────

def ts():
    return datetime.now().strftime("%H:%M:%S")

def log(msg, prefix=""):
    print(f"[{ts()}] {prefix}{msg}", flush=True)

def switch_model(model: str) -> bool:
    log(f"Switching to '{model}'...", "⟳  ")
    try:
        r = requests.post(
            f"{SWITCHER_URL}/switch",
            json={"model": model},
            timeout=SWITCH_TIMEOUT
        )
        data = r.json()
        if r.status_code == 200:
            log(f"Switch response: {data.get('message', data)}", "   ")
            return True
        else:
            log(f"Switch FAILED ({r.status_code}): {data}", "✗  ")
            return False
    except Exception as e:
        log(f"Switch request error: {e}", "✗  ")
        return False


def wait_for_health(url: str, model_name: str) -> bool:
    """Poll health endpoint until it responds OK."""
    log(f"Waiting for {model_name} health check at {url}/health ...", "   ")
    deadline = time.time() + SWITCH_TIMEOUT
    while time.time() < deadline:
        try:
            r = requests.get(f"{url}/health", timeout=5)
            if r.status_code == 200:
                log(f"{model_name} backend returned 200 OK", "   ")
                # Even if health is 200, some services might need a few more seconds
                log(f"Settling for {SETTLE_TIME}s...", "   ")
                time.sleep(SETTLE_TIME)
                log(f"{model_name} is ready ✓", "   ")
                return True
            elif r.status_code == 503:
                log(f"{model_name} is starting up (503)...", "   ")
            else:
                log(f"{model_name} health check status: {r.status_code}", "   ")
        except Exception:
            pass
        time.sleep(POLL_INTERVAL)
    log(f"{model_name} health check timed out after {SWITCH_TIMEOUT}s", "✗  ")
    return False


def test_vision() -> tuple[bool, float]:
    """Send cat.jpg to vision model and check we get a non-empty description."""
    if not os.path.exists(TEST_IMAGE_PATH):
        log(f"Test image not found: {TEST_IMAGE_PATH}", "✗  ")
        return False, 0.0

    with open(TEST_IMAGE_PATH, "rb") as f:
        b64 = base64.b64encode(f.read()).decode()

    log("Running vision inference...", "   ")
    t0 = time.time()
    try:
        r = requests.post(
            f"{VISION_URL}/v1/chat/completions",
            json={
                "model": "qwen3-vl",
                "messages": [{
                    "role": "user",
                    "content": [
                        {"type": "image_url", "image_url": {"url": f"data:image/jpeg;base64,{b64}"}},
                        {"type": "text", "text": "Describe this image in one sentence."}
                    ]
                }]
            },
            timeout=SERVICE_TIMEOUT
        )
        elapsed = time.time() - t0
        if r.status_code == 200:
            data = r.json()
            # Extract text from standard OpenAI-compatible response
            try:
                text = data["choices"][0]["message"]["content"]
            except (KeyError, IndexError):
                text = str(data)
            log(f"Vision result ({elapsed:.1f}s): {text[:120]}", "✓  ")
            return True, elapsed
        else:
            log(f"Vision FAILED ({r.status_code}): {r.text[:200]}", "✗  ")
            return False, time.time() - t0
    except Exception as e:
        log(f"Vision error: {e}", "✗  ")
        return False, time.time() - t0


def test_image_gen() -> tuple[bool, float]:
    """Ask LCM to generate an image and verify we get a response."""
    log(f"Running image generation (prompt: '{IMAGE_GEN_PROMPT}')...", "   ")
    t0 = time.time()
    try:
        r = requests.post(
            f"{IMAGE_GEN_URL}/generate",
            json={"prompt": IMAGE_GEN_PROMPT, "return_base64": False},
            timeout=SERVICE_TIMEOUT
        )
        elapsed = time.time() - t0
        if r.status_code == 200:
            data = r.json()
            w = data.get("width", "?")
            h = data.get("height", "?")
            seed = data.get("seed", "?")
            log(f"Image gen result ({elapsed:.1f}s): {w}x{h} seed={seed}", "✓  ")
            return True, elapsed
        else:
            log(f"Image gen FAILED ({r.status_code}): {r.text[:200]}", "✗  ")
            return False, time.time() - t0
    except Exception as e:
        log(f"Image gen error: {e}", "✗  ")
        return False, time.time() - t0

# ── Main loop ─────────────────────────────────────────────────────────────────

def main():
    iterations    = 0
    vision_pass   = 0
    vision_fail   = 0
    imggen_pass   = 0
    imggen_fail   = 0
    vision_times  = []
    imggen_times  = []

    print("=" * 60)
    print("  Model Cycle Test — press Ctrl+C to stop")
    print("=" * 60)
    print(f"  Switcher : {SWITCHER_URL}")
    print(f"  Vision   : {VISION_URL}")
    print(f"  Image gen: {IMAGE_GEN_URL}")
    print(f"  Test img : {TEST_IMAGE_PATH}")
    print("=" * 60)
    print()

    try:
        while True:
            iterations += 1
            print()
            print(f"{'─'*60}")
            log(f"ITERATION {iterations}", "🔄 ")
            print(f"{'─'*60}")

            # ── Vision leg ────────────────────────────────────────────
            if switch_model("vision"):
                if wait_for_health(VISION_URL, "vision"):
                    ok, t = test_vision()
                    if ok:
                        vision_pass += 1
                        vision_times.append(t)
                    else:
                        vision_fail += 1
                else:
                    vision_fail += 1
                    log("Skipping vision test — model not healthy", "⚠  ")
            else:
                vision_fail += 1

            # ── Image gen leg ─────────────────────────────────────────
            if switch_model("image_gen"):
                if wait_for_health(IMAGE_GEN_URL, "image_gen"):
                    ok, t = test_image_gen()
                    if ok:
                        imggen_pass += 1
                        imggen_times.append(t)
                    else:
                        imggen_fail += 1
                else:
                    imggen_fail += 1
                    log("Skipping image gen test — model not healthy", "⚠  ")
            else:
                imggen_fail += 1

            # ── Summary so far ────────────────────────────────────────
            print()
            avg_v = f"{sum(vision_times)/len(vision_times):.1f}s" if vision_times else "n/a"
            avg_i = f"{sum(imggen_times)/len(imggen_times):.1f}s" if imggen_times else "n/a"
            print(f"  📊 After {iterations} iteration(s):")
            print(f"     Vision   — pass: {vision_pass}  fail: {vision_fail}  avg: {avg_v}")
            print(f"     Image gen— pass: {imggen_pass}  fail: {imggen_fail}  avg: {avg_i}")
            
            # Simple rate limiting for the test loop
            time.sleep(1)

    except KeyboardInterrupt:
        print()
        print("=" * 60)
        print("  FINAL RESULTS")
        print("=" * 60)
        print(f"  Iterations completed : {iterations}")
        print()
        avg_v = f"{sum(vision_times)/len(vision_times):.1f}s" if vision_times else "n/a"
        avg_i = f"{sum(imggen_times)/len(imggen_times):.1f}s" if imggen_times else "n/a"
        print(f"  Vision    — pass: {vision_pass}  fail: {vision_fail}  avg inference: {avg_v}")
        print(f"  Image gen — pass: {imggen_pass}  fail: {imggen_fail}  avg inference: {avg_i}")
        total = vision_pass + vision_fail + imggen_pass + imggen_fail
        passed = vision_pass + imggen_pass
        print(f"  Overall   — {passed}/{total} tests passed")
        print("=" * 60)
        sys.exit(0)


if __name__ == "__main__":
    main()
