#!/bin/bash

# 1. Kill any existing instances
pkill -f chatbot-ui.py
pkill -x chatbot
pkill -9 sox
pkill -9 play
pkill -9 aplay

# 2. Get sound card index
card_index=$(grep -i "wm8960" /proc/asound/cards | head -n1 | awk '{print $1}')
if [ -z "$card_index" ]; then
  # Fallback to standard detection or default to 1
  card_index=$(awk '/wm8960soundcard/ {print $1}' /proc/asound/cards | head -n1)
  if [ -z "$card_index" ]; then card_index=1; fi
fi
echo "Detected WM8960 at Card Index: $card_index"
export SOUND_CARD_INDEX=$card_index

# 2.5 Setup WM8960 controls
echo "Configuring sound card $card_index (WM8960)..."
# Speaker/Playback setup
amixer -c $card_index set 'Speaker' 120
amixer -c $card_index sset 'Speaker' on
amixer -c $card_index sset 'Headphone' on
amixer -c $card_index sset 'Playback' on
amixer -c $card_index sset 'Left Output Mixer PCM' on
amixer -c $card_index sset 'Right Output Mixer PCM' on
amixer -c $card_index sset 'Mono Output Mixer Left' on
amixer -c $card_index sset 'Mono Output Mixer Right' on
# Recording/Mic setup
amixer -c $card_index set 'Capture' 70%
amixer -c $card_index set 'ADC PCM' 70%
amixer -c $card_index set 'Left Boost Mixer LINPUT1' on
amixer -c $card_index set 'Right Boost Mixer RINPUT1' on
amixer -c $card_index set 'Left Input Mixer Boost' on
amixer -c $card_index set 'Right Input Mixer Boost' on

# 3. Find .env file
if [ -f "./.env" ]; then
  ENV_FILE=$(realpath "./.env")
elif [ -f "../.env" ]; then
  ENV_FILE=$(realpath "../.env")
elif [ -f "$HOME/.env" ]; then
  ENV_FILE=$(realpath "$HOME/.env")
fi

if [ -n "$ENV_FILE" ]; then
  echo "Loading environment from $ENV_FILE"
  # Export all variables from .env
  set -a
  source "$ENV_FILE"
  set +a
  
  # Only match lines that start with CUSTOM_FONT_PATH (ignore comments)
  FONT_PATH_RAW=$(grep "^CUSTOM_FONT_PATH=" "$ENV_FILE" | cut -d '=' -f2 | tr -d '"' | tr -d "'" | tr -d ' ')
  if [ -n "$FONT_PATH_RAW" ]; then
      # If it's a relative path in .env, make it relative to the .env file location
      if [[ "$FONT_PATH_RAW" != /* ]]; then
          ENV_DIR=$(dirname "$ENV_FILE")
          FONT_PATH_CANDIDATE=$(realpath "$ENV_DIR/$FONT_PATH_RAW" 2>/dev/null)
      else
          FONT_PATH_CANDIDATE="$FONT_PATH_RAW"
      fi
      
      # Verify if the file exists
      if [ -f "$FONT_PATH_CANDIDATE" ]; then
          FONT_PATH="$FONT_PATH_CANDIDATE"
      else
          echo "Warning: Font path from .env does not exist: $FONT_PATH_CANDIDATE"
      fi
  fi
fi

if [ -z "$FONT_PATH" ]; then
  # Fallback to default locations
  echo "Searching for default font..."
  if [ -f "./python/NotoSansSC-Bold.ttf" ]; then
    FONT_PATH=$(realpath "./python/NotoSansSC-Bold.ttf")
  elif [ -f "../python/NotoSansSC-Bold.ttf" ]; then
    FONT_PATH=$(realpath "../python/NotoSansSC-Bold.ttf")
  elif [ -f "./NotoSansSC-Bold.ttf" ]; then
    FONT_PATH=$(realpath "./NotoSansSC-Bold.ttf")
  elif [ -f "/home/${USER}/python/NotoSansSC-Bold.ttf" ]; then
    FONT_PATH="/home/${USER}/python/NotoSansSC-Bold.ttf"
  fi
fi

if [ -n "$FONT_PATH" ]; then
  export CUSTOM_FONT_PATH=$FONT_PATH
  echo "Using Font: $CUSTOM_FONT_PATH"
else
  echo "Error: No font found. Please check your .env or python/ directory."
fi

# 4. Start Python UI server in background
if [ -d "./python" ]; then
  PYTHON_DIR="./python"
elif [ -d "../python" ]; then
  PYTHON_DIR="../python"
else
  PYTHON_DIR="." # Assume we are in python dir
fi

echo "Starting UI Server from $PYTHON_DIR..."
cd "$PYTHON_DIR"
/usr/bin/python3 chatbot-ui.py &
PYTHON_PID=$!
cd - > /dev/null

# 5. Run Go chatbot
if [ -f "./chatbot" ]; then
  ./chatbot
elif [ -f "../bin/chatbot" ]; then
  ../bin/chatbot
else
  echo "Error: Chatbot binary not found. Try running 'make build-chatbot' from the project root."
  exit 1
fi

# 6. Cleanup
kill $PYTHON_PID
