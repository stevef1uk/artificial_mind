#!/bin/sh
set -euo pipefail

# Determine the encrypted file and binary name based on the service
if [ -f /app/principles.enc ]; then
  ZIP_PATH=/app/principles.enc
  BIN_NAME=principles-server
elif [ -f /app/hdn-server.enc ]; then
  ZIP_PATH=/app/hdn-server.enc
  BIN_NAME=hdn-server
elif [ -f /app/goal-manager.enc ]; then
  ZIP_PATH=/app/goal-manager.enc
  BIN_NAME=goal-manager
elif [ -f /app/data-processor.enc ]; then
  ZIP_PATH=/app/data-processor.enc
  BIN_NAME=data-processor
elif [ -f /app/knowledge-builder.enc ]; then
  ZIP_PATH=/app/knowledge-builder.enc
  BIN_NAME=knowledge-builder
elif [ -f /app/fsm-server.enc ]; then
  ZIP_PATH=/app/fsm-server.enc
  BIN_NAME=fsm-server
elif [ -f /app/monitor-ui.enc ]; then
  ZIP_PATH=/app/monitor-ui.enc
  BIN_NAME=monitor-ui
elif [ -f /app/wiki-summarizer.enc ]; then
  ZIP_PATH=/app/wiki-summarizer.enc
  BIN_NAME=wiki-summarizer
else
  echo "No encrypted payload found. Expected one of: principles.enc, hdn-server.enc, goal-manager.enc, data-processor.enc, knowledge-builder.enc, fsm-server.enc, monitor-ui.enc, wiki-summarizer.enc" >&2
  exit 1
fi

OUT_DIR=/tmp/dec
WORK_DIR="${UNPACK_WORK_DIR:-/tmp/unpack}"
PRIV_PATH="${SECURE_CUSTOMER_PRIVATE_PATH:-/keys/customer_private.pem}"

if [ ! -f "$ZIP_PATH" ]; then
  echo "Encrypted payload $ZIP_PATH not found" >&2
  exit 1
fi
if ! command -v unpack >/dev/null 2>&1; then
  echo "unpack tool not found in PATH" >&2
  exit 1
fi
if [ ! -f "$PRIV_PATH" ]; then
  echo "Customer private key not found at $PRIV_PATH" >&2
  exit 1
fi

mkdir -p "$OUT_DIR" "$WORK_DIR"

# Optional vendor token (required if package was created with -license)
TOKEN_ARG=""
if [ -n "${SECURE_VENDOR_TOKEN:-}" ]; then
  TOKEN_FILE="/tmp/vendor.token"
  printf "%s" "$SECURE_VENDOR_TOKEN" > "$TOKEN_FILE"
  TOKEN_ARG="-license-token $TOKEN_FILE"
fi

# Unpack the encrypted zip created by packager (-zip=true)
unpack -zip "$ZIP_PATH" -priv "$PRIV_PATH" -out "$OUT_DIR" -work "$WORK_DIR" $TOKEN_ARG

BIN_PATH="$OUT_DIR/$BIN_NAME"
if [ ! -x "$BIN_PATH" ]; then
  if [ -f "$BIN_PATH" ]; then chmod +x "$BIN_PATH"; fi
fi
if [ ! -x "$BIN_PATH" ]; then
  echo "Unpacked binary not found at $BIN_PATH" >&2
  ls -la "$OUT_DIR" || true
  exit 1
fi

# For monitor-ui, extract the complete tar file
if [ "$BIN_NAME" = "monitor-ui" ]; then
  echo "Extracting monitor-ui complete payload..."
  if [ -f "$OUT_DIR/monitor-ui.tar.gz" ]; then
    # Extract to a temporary directory first
    mkdir -p "$OUT_DIR/temp-extract"
    cd "$OUT_DIR/temp-extract"
    tar -xzf "../monitor-ui.tar.gz"
    
    # Move the extracted files to the correct location
    # The monitor-ui binary expects templates/ and static/ to be in a 'monitor' subdirectory
    mkdir -p "$OUT_DIR/monitor"
    mv templates "$OUT_DIR/monitor/"
    mv static "$OUT_DIR/monitor/"
    
    # Clean up temp directory
    cd "$OUT_DIR"
    rm -rf temp-extract
    
    # Create symlink so monitor-ui binary can find templates in expected location
    ln -sf "$OUT_DIR/monitor" "/tmp/monitor"
    
    echo "Monitor-ui complete payload extracted with templates and static files in correct structure"
  else
    echo "Warning: monitor-ui.tar.gz not found in $OUT_DIR"
  fi
fi

echo "DEBUG: About to execute $BIN_PATH with arguments: $@"
exec "$BIN_PATH" "$@"
