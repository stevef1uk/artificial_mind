#!/bin/bash
set -e

CODEGEN_URL=${CODEGEN_URL:-https://google.com}
OUTPUT_PATH=${OUTPUT_PATH:-/output/codegen.ts}

mkdir -p /output ~/.vnc

echo "🚀 Starting Playwright codegen environment..."

# 1. Start Xvfb (Virtual Framebuffer)
# Use a common resolution
Xvfb :99 -ac -screen 0 1920x1080x24 >/tmp/xvfb.log 2>&1 &
XVFB_PID=$!
sleep 2
echo "✓ Xvfb started (PID: $XVFB_PID) on :99"

# 2. Start Fluxbox (Window Manager)
# Needs DISPLAY to know where to run
export DISPLAY=:99
fluxbox >/tmp/fluxbox.log 2>&1 &
FLUXBOX_PID=$!
echo "✓ Fluxbox started (PID: $FLUXBOX_PID)"

# 3. Start x11vnc (the VNC server itself, connecting to the Xvfb display)
# -forever keeps it alive, -nopw for simplicity (it's internal to docker)
x11vnc -display :99 -forever -nopw -bg -rfbport 5900 -shared >/tmp/vnc.log 2>&1 &
VNC_PID=$!
echo "✓ VNC server started (PID: $VNC_PID) on port 5900"

# 4. Start NoVNC (Websockets proxy to VNC)
# NoVNC needs to listen on 6080 and talk to port 5900
/usr/share/novnc/utils/novnc_proxy --vnc localhost:5900 --listen 6080 >/tmp/novnc.log 2>&1 &
NOVNC_PID=$!
echo "✓ NoVNC started (PID: $NOVNC_PID) at http://localhost:7000/vnc.html"

sleep 2

echo "✅ VNC environment ready."
echo "🎬 To start Playwright codegen, run:"
echo "   playwright codegen --browser=chromium --output $OUTPUT_PATH $CODEGEN_URL"

# Keep container alive
tail -f /dev/null
