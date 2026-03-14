from PIL import Image
from whisplay import WhisplayBoard
import sys
import time
import threading
import requests
import os
import io
from utils import ImageUtils


class CameraThread(threading.Thread):
    
    picam2 = None

    def __init__(self, whisplay, image_path):
        super().__init__()
        self.whisplay = whisplay
        self.remote_url = os.getenv("REMOTE_CAMERA_URL")
        
        if not self.remote_url:
            # Only import Picamera2 if we're using local camera
            from picamera2 import Picamera2
            if CameraThread.picam2 is None:
                CameraThread.picam2 = Picamera2()
                CameraThread.picam2.configure(CameraThread.picam2.create_preview_configuration(main={"size": (self.whisplay.LCD_WIDTH, self.whisplay.LCD_HEIGHT)}))
            CameraThread.picam2.start()
            print("[Camera v3] Initialized local camera")
        else:
            # Check if remote camera server is available
            print(f"[Camera v3] Checking remote camera at {self.remote_url} ...")
            try:
                # Use a longer timeout for the initial health check
                health_check = requests.get(f"{self.remote_url}/preview", timeout=5)
                if health_check.status_code != 200:
                    raise Exception(f"Status {health_check.status_code}")
                print(f"[Camera v3] Remote camera server confirmed at {self.remote_url}")
            except Exception as e:
                error_msg = f"Remote camera at {self.remote_url} check failed: {str(e)}"
                print(f"[Camera v3] ERROR: {error_msg}")
                raise Exception(error_msg)



        self.running = False
        self.capture_image = None
        self.image_path = image_path
        self.consecutive_errors = 0
        self.max_consecutive_errors = 5
        self.error_exit = False  # Track if we exited due to errors
        
    def start(self):
        self.running = True
        return super().start()

    def run(self):
        print(f"[Camera v3] Preview thread starting: running={self.running}, remote={self.remote_url}")
        while self.running:
            # If we just captured an image, show it for 1 second as feedback
            if self.capture_image is not None:
                print(f"[Camera v3] Displaying capture feedback...")
                pixel_bytes = ImageUtils.image_to_rgb565(self.capture_image, self.whisplay.LCD_WIDTH, self.whisplay.LCD_HEIGHT)
                self.whisplay.draw_image(0, 0, self.whisplay.LCD_WIDTH, self.whisplay.LCD_HEIGHT, pixel_bytes)
                time.sleep(1.0)
                self.capture_image = None # Reset to resume preview
                continue

            try:
                if self.remote_url:
                    # Fetch from remote
                    preview_url = f"{self.remote_url}/preview"
                    # print(f"[Camera v3] Fetching preview from {preview_url}")
                    response = requests.get(preview_url, timeout=2)
                    if response.status_code == 200:
                        # Ensure we actually got full image data
                        if len(response.content) < 1000:
                             print(f"[Camera v3] Received small packet ({len(response.content)} bytes). Metadata?")
                             time.sleep(0.5)
                             continue
                             
                        image = Image.open(io.BytesIO(response.content))
                        
                        if image.size != (self.whisplay.LCD_WIDTH, self.whisplay.LCD_HEIGHT):
                             image = image.resize((self.whisplay.LCD_WIDTH, self.whisplay.LCD_HEIGHT), Image.LANCZOS)
                        
                        if image.mode != "RGB":
                            image = image.convert("RGB")
                            
                        pixel_bytes = ImageUtils.image_to_rgb565(image, self.whisplay.LCD_WIDTH, self.whisplay.LCD_HEIGHT)
                        # Reset error counter on success
                        self.consecutive_errors = 0
                    else:
                        print(f"[Camera v3] Remote preview failed: {response.status_code}")
                        self.consecutive_errors += 1
                        if self.consecutive_errors >= self.max_consecutive_errors:
                            print(f"[Camera v3] Too many consecutive errors ({self.consecutive_errors}), exiting camera mode")
                            self.error_exit = True
                            break
                        time.sleep(1)
                        continue
                else:
                    # Local camera
                    frame = CameraThread.picam2.capture_array()
                    pixel_bytes = ImageUtils.convertCameraFrameToRGB565(frame, self.whisplay.LCD_WIDTH, self.whisplay.LCD_HEIGHT)
                    self.consecutive_errors = 0
                
                self.whisplay.draw_image(0, 0, self.whisplay.LCD_WIDTH, self.whisplay.LCD_HEIGHT, pixel_bytes)
                
                # Check condition again before sleeping
                if not self.running:
                    break
                    
                # Throttle to ~10 FPS to reduce server load
                time.sleep(0.1)
            except Exception as e:
                print(f"[Camera v3] Error in preview loop: {e}")
                self.consecutive_errors += 1
                if self.consecutive_errors >= self.max_consecutive_errors:
                    print(f"[Camera v3] Too many consecutive errors ({self.consecutive_errors}), exiting camera mode")
                    self.error_exit = True
                    break
                time.sleep(1)

        print(f"[Camera v3] Preview loop ended (running={self.running})")
                
    def capture(self):
        try:
            if self.remote_url:
                 response = requests.get(f"{self.remote_url}/capture", timeout=5)
                 if response.status_code == 200:
                     self.capture_image = Image.open(io.BytesIO(response.content))
                 else:
                     print(f"[Camera] Remote capture failed: {response.status_code}")
                     return
            else:
                frame = CameraThread.picam2.capture_array()
                self.capture_image = Image.fromarray(frame)
            
            # convert to RGB to avoid errors when saving as JPEG (JPEG does not support alpha)
            if self.capture_image.mode != "RGB":
                self.capture_image = self.capture_image.convert("RGB")
            # save to file
            self.capture_image.save(self.image_path, format="JPEG", quality=95)
            print(f"[Camera] Captured image saved to {self.image_path}")
        except Exception as e:
            print(f"[Camera] Capture failed: {e}")

    def stop(self):
        print("[Camera v3] stop() called on preview thread")
        self.running = False
        if not self.remote_url and self.picam2:
            self.picam2.stop()
        self.join(timeout=3)
        print("[Camera v3] preview thread joined")


if __name__ == "__main__":
    whisplay = WhisplayBoard()
    print(f"[LCD] Initialization finished: {whisplay.LCD_WIDTH}x{whisplay.LCD_HEIGHT}")
    
    
    def cleanup_and_exit(signum, frame):
        print("[System] Exiting...")
        whisplay.cleanup()
        sys.exit(0)
        
    picam2 = Picamera2()
    picam2.configure(picam2.create_preview_configuration(main={"size": (whisplay.LCD_WIDTH * 2, whisplay.LCD_HEIGHT * 2)}))
    picam2.start()
    whisplay.set_backlight(100)
    time.sleep(2)  # Allow camera to warm up

    try:
        
        # Keep the main thread alive
        while True:
            # Capture image from Pi Camera
            start_time = time.time()
            frame = picam2.capture_array()
            pixel_bytes = ImageUtils.convertCameraFrameToRGB565(frame, whisplay.LCD_WIDTH, whisplay.LCD_HEIGHT)
            
            # Send the pixel data to the display
            whisplay.draw_image(0, 0, whisplay.LCD_WIDTH, whisplay.LCD_HEIGHT, pixel_bytes)
            end_time = time.time()
            fps = 1 / (end_time - start_time)
            print(f"[Camera] Displayed frame at {fps:.2f} FPS")
    except KeyboardInterrupt:
        picam2.stop()
        cleanup_and_exit(None, None)


