import time
import io
import threading
from flask import Flask, Response
from picamera2 import Picamera2
from PIL import Image

app = Flask(__name__)

# Initialize PiCamera2
picam2 = Picamera2()
# Use a standard preview resolution
config = picam2.create_preview_configuration(main={"size": (640, 480)})
picam2.configure(config)
picam2.start()

@app.route('/health')
def health():
    return "OK", 200

@app.route('/preview')
@app.route('/capture')
def capture_image():
    """Captures an image, converts it to JPEG, and returns the bytes."""
    try:
        # 1. Capture to a numpy array
        frame = picam2.capture_array()
        
        # 2. Convert array to PIL Image and ensure it is in RGB mode
        img = Image.fromarray(frame).convert('RGB')
        
        # 3. Save to a JPEG bytes buffer
        stream = io.BytesIO()
        img.save(stream, format='JPEG', quality=85)
        
        print(f"Captured and sent image: {len(stream.getvalue())} bytes")
        return Response(stream.getvalue(), mimetype='image/jpeg')
        
    except Exception as e:
        print(f"Capture error: {e}")
        return str(e), 500

if __name__ == '__main__':
    # Flask by default uses a lot of memory per thread, 
    # but for a simple camera server it's okay.
    app.run(host='0.0.0.0', port=5000, threaded=True)
