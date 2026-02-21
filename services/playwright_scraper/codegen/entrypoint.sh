#!/bin/bash
set -e

CODEGEN_URL=${CODEGEN_URL:-https://example.com}
OUTPUT_PATH=${OUTPUT_PATH:-/output/codegen.ts}

mkdir -p /output ~/.vnc

echo "🚀 Starting Playwright codegen environment..."

# Create vnc password config (empty password)
mkdir -p ~/.vnc
echo "#!/bin/bash" > ~/.vnc/xstartup
echo "exec fluxbox" >> ~/.vnc/xstartup
chmod +x ~/.vnc/xstartup

# Start TigerVNC server with its own X display
vncserver :99 -geometry 1920x1080 -depth 24 -SecurityTypes none >/tmp/vnc.log 2>&1 &
VNC_PID=$!
sleep 2
echo "✓ VNC server started (PID: $VNC_PID) on :99"

websockify --web=/usr/share/novnc/ 6080 127.0.0.1:5999 >/tmp/novnc.log 2>&1 &
WEBSOCKIFY_PID=$!
echo "✓ Websockify started (PID: $WEBSOCKIFY_PID)"

sleep 2

echo "✅ VNC environment ready at http://localhost:7000/vnc.html"
echo "🎬 To start Playwright codegen, run:"
echo "   npx playwright codegen --output $OUTPUT_PATH $CODEGEN_URL"


# Keep container alive
tail -f /dev/null
