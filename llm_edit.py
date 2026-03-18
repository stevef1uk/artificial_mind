#!/usr/bin/env python3
"""
llm_edit.py — Edit any file using the Qwen3 LLM running on the LLM8850 NPU.

Usage:
  python3 llm_edit.py <file> "<instruction>"

Examples:
  python3 llm_edit.py .env "Change the system prompt to be more concise"
  python3 llm_edit.py serve.sh "Add error handling if the tokenizer server fails to start"
  python3 llm_edit.py config.py "Rename LLM_HOST to QWEN_HOST throughout"

The original file is backed up as <file>.bak before any changes are written.
"""

import sys
import json
import urllib.request
import urllib.error
import shutil
import os
import base64
import http.client

# ── Config ────────────────────────────────────────────────────────────────────
QWEN3_HOST = os.environ.get("LLM8850_LLM_HOST", "http://localhost:8000")
API_URL     = f"{QWEN3_HOST}/v1/chat/completions"
MODEL       = "qwen3"       # adjust if your endpoint uses a different model name
TEMPERATURE = float(os.environ.get("LLM8850_LLM_TEMPERATURE", "0.3"))
MAX_TOKENS  = 4096
# Image generator config
GEN_HOST = os.environ.get("WHISPLAY_GEN_HOST", "192.168.1.60")
GEN_PORT = int(os.environ.get("WHISPLAY_GEN_PORT", "8806"))
# ─────────────────────────────────────────────────────────────────────────────

SYSTEM_PROMPT = """\
You are a precise file-editing assistant. The user will give you the full
contents of a file and an instruction describing the change they want.

Rules:
1. Reply with ONLY the complete, updated file contents — no explanation,
   no markdown fences, no preamble, no commentary.
2. Preserve all whitespace, indentation style, and line endings exactly
   as in the original, except where the edit requires a change.
3. If the instruction is ambiguous, make the most conservative interpretation.
4. If the file should not be changed (instruction already satisfied),
   return the original contents unchanged.
"""


def call_qwen3(file_contents: str, instruction: str) -> str:
    user_message = (
        f"File contents:\n{file_contents}\n\n"
        f"Instruction: {instruction}"
    )

    payload = json.dumps({
        "model": MODEL,
        "temperature": TEMPERATURE,
        "max_tokens": MAX_TOKENS,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user",   "content": user_message},
        ],
    }).encode("utf-8")

    req = urllib.request.Request(
        API_URL,
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )

    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            data = json.loads(resp.read().decode("utf-8"))
    except urllib.error.URLError as e:
        print(f"[error] Could not reach Qwen3 API at {API_URL}: {e}")
        print(f"        Check that the LLM8850 service is running and that")
        print(f"        LLM8850_LLM_HOST is set correctly (current: {QWEN3_HOST})")
        sys.exit(1)

    # OpenAI-compatible response shape
    try:
        return data["choices"][0]["message"]["content"]
    except (KeyError, IndexError) as e:
        print(f"[error] Unexpected API response shape: {e}")
        print(json.dumps(data, indent=2))
        sys.exit(1)


def main():
    if len(sys.argv) < 3:
        print("Usage: python3 llm_edit.py <file> \"<instruction>\"")
        print("       python3 llm_edit.py --image <image_file> \"<prompt>\"")
        sys.exit(1)

    if sys.argv[1] == "--image":
        # Image editing mode
        image_path = sys.argv[2]
        prompt = " ".join(sys.argv[3:])
        if not os.path.isfile(image_path):
            print(f"[error] Image file not found: {image_path}")
            sys.exit(1)
        print(f"[info]  Editing image: {image_path}")
        print(f"[info]  Prompt: {prompt}")
        print(f"[info]  Calling image generator at {GEN_HOST}:{GEN_PORT} ...")

        # Prepare payload
        payload_data = {"prompt": prompt, "return_base64": True}
        with open(image_path, "rb") as f:
            payload_data["init_image"] = base64.b64encode(f.read()).decode()
        payload_data["denoising_strength"] = 0.5
        payload = json.dumps(payload_data)
        headers = {"Content-Type": "application/json"}

        conn = http.client.HTTPConnection(GEN_HOST, GEN_PORT)
        conn.request("POST", "/generate", payload, headers)
        res = conn.getresponse()
        data = json.loads(res.read().decode())

        if "image_base64" not in data:
            print(f"[error] API failed to return image: {data}")
            sys.exit(1)

        img_data = base64.b64decode(data["image_base64"])
        out_path = image_path.replace(".jpg", "_edited.png").replace(".png", "_edited.png")
        with open(out_path, "wb") as f:
            f.write(img_data)
        print(f"[info]  Edited image saved: {out_path}")
        print(f"[info]  Seed: {data.get('seed', 0)}")
        sys.exit(0)

    # Default: file editing mode
    filepath    = sys.argv[1]
    instruction = " ".join(sys.argv[2:])

    if not os.path.isfile(filepath):
        print(f"[error] File not found: {filepath}")
        sys.exit(1)

    with open(filepath, "r", encoding="utf-8") as f:
        original = f.read()

    print(f"[info]  Reading:     {filepath} ({len(original)} chars)")
    print(f"[info]  Instruction: {instruction}")
    print(f"[info]  Calling Qwen3 at {API_URL} ...")

    updated = call_qwen3(original, instruction)

    # Strip accidental markdown fences the model might add despite the prompt
    if updated.startswith("```"):
        lines = updated.splitlines()
        # remove first and last fence lines
        if lines[0].startswith("```"):
            lines = lines[1:]
        if lines and lines[-1].strip() == "```":
            lines = lines[:-1]
        updated = "\n".join(lines)

    if updated == original:
        print("[info]  No changes were made (file already satisfies the instruction).")
        sys.exit(0)

    # Back up the original
    backup = filepath + ".bak"
    shutil.copy2(filepath, backup)
    print(f"[info]  Backup saved: {backup}")

    with open(filepath, "w", encoding="utf-8") as f:
        f.write(updated)

    print(f"[info]  File updated: {filepath}")

    # Show a simple diff summary
    orig_lines    = original.splitlines()
    updated_lines = updated.splitlines()
    added   = sum(1 for l in updated_lines if l not in orig_lines)
    removed = sum(1 for l in orig_lines    if l not in updated_lines)
    print(f"[diff]  ~{added} lines added, ~{removed} lines removed")


if __name__ == "__main__":
    main()
