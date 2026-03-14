from PIL import Image
from whisplay import WhisplayBoard
import requests
import os
import io
import sys
import time
from utils import ImageUtils

def main():
    # 1. Configuration
    remote_url = os.getenv("REMOTE_CAMERA_URL")
    if not remote_url:
        print("Error: REMOTE_CAMERA_URL environment variable is not set.")
        print("Example: export REMOTE_CAMERA_URL=http://192.168.1.68:5000")
        sys.exit(1)

    print(f"Connecting to Whisplay Board...")
    whisplay = WhisplayBoard()
    
    # TEST PATTERN
    print("Initializing display with RED test pattern...")
    whisplay.set_backlight(100)
    # Fill with red color (0xF800 in RGB565)
    red_pixel = b'\xF8\x00' * (whisplay.LCD_WIDTH * whisplay.LCD_HEIGHT)
    whisplay.draw_image(0, 0, whisplay.LCD_WIDTH, whisplay.LCD_HEIGHT, red_pixel)
    time.sleep(2)
    print("Test pattern done. Fetching camera...")
    
    # 2. Fetch Image
    capture_url = f"{remote_url}/capture"
    print(f"Fetching capture from {capture_url}...")
    
    try:
        response = requests.get(capture_url, timeout=10)
        if response.status_code == 200:
            print(f"Successfully received {len(response.content)} bytes of image data.")
            try:
                image = Image.open(io.BytesIO(response.content))
            except Exception as img_err:
                print(f"Error: cannot identify image file. Raw data starts with: {response.content[:50]}")
                raise img_err
            
            # 3. Process and Resize
            print(f"Image original size: {image.size}")
            if image.size != (whisplay.LCD_WIDTH, whisplay.LCD_HEIGHT):
                 print(f"Resizing to {whisplay.LCD_WIDTH}x{whisplay.LCD_HEIGHT}...")
                 # Use Image.LANCZOS for high quality downsampling
                 image = image.resize((whisplay.LCD_WIDTH, whisplay.LCD_HEIGHT), Image.LANCZOS)
            
            # 4. Convert to RGB565
            if image.mode != "RGB":
                image = image.convert("RGB")
            
            pixel_bytes = ImageUtils.image_to_rgb565(image, whisplay.LCD_WIDTH, whisplay.LCD_HEIGHT)
            
            # 5. Display
            print("Drawing to display...")
            whisplay.draw_image(0, 0, whisplay.LCD_WIDTH, whisplay.LCD_HEIGHT, pixel_bytes)
            print("Done! Displaying for 10 seconds...")
            time.sleep(10)
            
        else:
            print(f"Failed to fetch image. Status code: {response.status_code}")
            print(f"Server Error Message:\n{response.text}")
            
    except Exception as e:
        print(f"An error occurred: {e}")
    finally:
        whisplay.cleanup()

if __name__ == "__main__":
    main()
