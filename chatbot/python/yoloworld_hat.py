import colorsys
import re
import subprocess
import cv2
import requests
import io
import time
import datetime
import os
import sys
from pyaxdev import enum_devices, sys_init, sys_deinit, AxDeviceType
from pyyoloworld import YOLOWORLD
import numpy as np
from PIL import Image, ImageDraw
import argparse
import random
import spidev

try:
    import RPi.GPIO as GPIO
except ImportError:
    print("Error: RPi.GPIO not found. If on Pi 5, please install 'rpi-lgpio'.")
    sys.exit(1)

class ImageUtils:
    @staticmethod
    def image_to_rgb565(image: Image.Image, width: int, height: int) -> list:
        image = image.convert("RGB")
        image.thumbnail((width, height), Image.LANCZOS)
        bg = Image.new("RGB", (width, height), (0, 0, 0))
        x, y = (width - image.width) // 2, (height - image.height) // 2
        bg.paste(image, (x, y))
        np_img = np.array(bg)
        r = (np_img[:, :, 0] >> 3).astype(np.uint16)
        g = (np_img[:, :, 1] >> 2).astype(np.uint16)
        b = (np_img[:, :, 2] >> 3).astype(np.uint16)
        rgb565 = (r << 11) | (g << 5) | b
        high_byte = (rgb565 >> 8).astype(np.uint8)
        low_byte = (rgb565 & 0xFF).astype(np.uint8)
        interleaved = np.dstack((high_byte, low_byte)).flatten().tolist()
        return interleaved

class WhisplayBoard:
    LCD_WIDTH = 240
    LCD_HEIGHT = 280
    DC_PIN = 13
    RST_PIN = 7
    LED_PIN = 15
    BUTTON_PIN = 11
    RED_PIN = 22
    GREEN_PIN = 18
    BLUE_PIN = 16

    def __init__(self):
        GPIO.setmode(GPIO.BOARD)
        GPIO.setwarnings(False)
        GPIO.setup([self.DC_PIN, self.RST_PIN, self.LED_PIN], GPIO.OUT)
        GPIO.output(self.LED_PIN, GPIO.LOW)
        self.backlight_pwm = GPIO.PWM(self.LED_PIN, 1000)
        self.backlight_pwm.start(100)
        GPIO.setup([self.RED_PIN, self.GREEN_PIN, self.BLUE_PIN], GPIO.OUT)
        self.red_pwm = GPIO.PWM(self.RED_PIN, 100)
        self.green_pwm = GPIO.PWM(self.GREEN_PIN, 100)
        self.blue_pwm = GPIO.PWM(self.BLUE_PIN, 100)
        self.red_pwm.start(0)
        self.green_pwm.start(0)
        self.blue_pwm.start(0)
        self.spi = spidev.SpiDev()
        self.spi.open(0, 0)
        self.spi.max_speed_hz = 100_000_000
        self.spi.mode = 0b00
        self._reset_lcd()
        self._init_display()
        self.fill_screen(0)

    def set_backlight(self, brightness):
        if 0 <= brightness <= 100:
            self.backlight_pwm.ChangeDutyCycle(100 - brightness)

    def _reset_lcd(self):
        GPIO.output(self.RST_PIN, GPIO.HIGH)
        time.sleep(0.1)
        GPIO.output(self.RST_PIN, GPIO.LOW)
        time.sleep(0.1)
        GPIO.output(self.RST_PIN, GPIO.HIGH)
        time.sleep(0.12)

    def _init_display(self):
        self._send_command(0x11); time.sleep(0.12)
        self._send_command(0x36, 0xC0)  # Horizontal mode
        self._send_command(0x3A, 0x05)
        self._send_command(0xB2, 0x0C, 0x0C, 0x00, 0x33, 0x33)
        for c, v in [(0xB7, 0x35), (0xBB, 0x32), (0xC2, 0x01), (0xC3, 0x15), (0xC4, 0x20), (0xC6, 0x0F)]: self._send_command(c, v)
        self._send_command(0xD0, 0xA4, 0xA1)
        gam = [0xD0, 0x08, 0x0E, 0x09, 0x09, 0x05, 0x31, 0x33, 0x48, 0x17, 0x14, 0x15, 0x31, 0x34]
        self._send_command(0xE0, *gam); self._send_command(0xE1, *gam)
        self._send_command(0x21); self._send_command(0x29)

    def _send_command(self, cmd, *args):
        GPIO.output(self.DC_PIN, GPIO.LOW)
        self.spi.xfer2([cmd])
        if args:
            GPIO.output(self.DC_PIN, GPIO.HIGH)
            self._send_data(list(args))

    def _send_data(self, data):
        GPIO.output(self.DC_PIN, GPIO.HIGH)
        try:
            self.spi.writebytes2(data)
        except AttributeError:
            max_chunk = 4096
            for i in range(0, len(data), max_chunk):
                self.spi.writebytes(data[i : i + max_chunk])

    def set_window(self, x0, y0, x1, y1):
        self._send_command(0x2A, x0 >> 8, x0 & 0xFF, x1 >> 8, x1 & 0xFF)
        self._send_command(0x2B, (y0 + 20) >> 8, (y0 + 20) & 0xFF, (y1 + 20) >> 8, (y1 + 20) & 0xFF)
        self._send_command(0x2C)

    def fill_screen(self, color):
        self.set_window(0, 0, self.LCD_WIDTH - 1, self.LCD_HEIGHT - 1)
        high, low = (color >> 8) & 0xFF, color & 0xFF
        buf = [high, low] * 4096
        for _ in range((self.LCD_WIDTH * self.LCD_HEIGHT * 2) // len(buf)):
            self._send_data(buf)

    def draw_image(self, x, y, width, height, data):
        self.set_window(x, y, x + width - 1, y + height - 1)
        self._send_data(data)

    def cleanup(self):
        self.spi.close(); [pwm.stop() for pwm in [self.red_pwm, self.green_pwm, self.blue_pwm]]; self.backlight_pwm.stop(); GPIO.cleanup()

parser = argparse.ArgumentParser()
parser.add_argument('--yoloworld', default='../yolo_u16_ax650.axmodel')
parser.add_argument('--tenc', default='../clip_b1_u16_ax650.axmodel')
parser.add_argument('--vocab', default='../vocab.txt')
parser.add_argument('--url', default='http://192.168.1.68:5000/capture')
parser.add_argument('--interval', type=float, default=0.2)
parser.add_argument('--classes', nargs='+', default=['person', 'cat', 'table', 'chair'])
parser.add_argument('--threshold', type=float, default=0.05)
args = parser.parse_args()

for p in [args.yoloworld, args.tenc, args.vocab]:
    if not os.path.exists(p): print(f"Error: {p} not found."); sys.exit(1)

devices = enum_devices()
if devices['devices']['count'] > 0: sys_init(AxDeviceType.axcl_device, 0); dev_type, dev_id = AxDeviceType.axcl_device, 0
elif devices['host']['available']: sys_init(AxDeviceType.host_device, -1); dev_type, dev_id = AxDeviceType.host_device, -1
else: print("No device available."); sys.exit(1)

yw = YOLOWORLD({'dev_type': dev_type, 'devid': dev_id, 'text_encoder_path': args.tenc, 'tokenizer_path': args.vocab, 'yoloworld_path': args.yoloworld})
class_list = args.classes
colors = [(int(r*255), int(g*255), int(b*255)) for r,g,b in [colorsys.hsv_to_rgb(i/len(class_list), 0.95, 0.95) for i in range(len(class_list))]]
yw.set_classes(class_list); yw.set_threshold(args.threshold)

print("Initializing Whisplay Board..."); whisplay = WhisplayBoard()
whisplay.set_backlight(100) # Enable backlight

def loop():
    try:
        resp = requests.get(args.url, timeout=3)
        if resp.status_code != 200: return
        img = Image.open(io.BytesIO(resp.content))
        img_np = np.array(img.convert('RGB'))
        results = yw.detect(img_np)
        for res in results:
            color = colors[res['label'] % len(colors)]
            x, y, w, h = res['x'], res['y'], res['w'], res['h']
            cv2.rectangle(img_np, (x, y), (x+w, y+h), color, 3)
            cv2.putText(img_np, class_list[res['label']], (x, y-5), cv2.FONT_HERSHEY_SIMPLEX, 1, color, 2)
        
        # Letterbox (Fit) display logic
        pil_img = Image.fromarray(img_np)
        canvas = Image.new("RGB", (240, 280), (0, 0, 0))
        pil_img.thumbnail((240, 280), Image.LANCZOS)
        # Center on canvas
        offset = ((240 - pil_img.width) // 2, (280 - pil_img.height) // 2)
        canvas.paste(pil_img, offset)
        
        whisplay.draw_image(0, 0, 240, 280, ImageUtils.image_to_rgb565(canvas, 240, 280))
    except Exception as e: print(f"Loop error: {e}")

try:
    print("Running detection loop (Ctrl+C to stop)...")
    while True:
        t = time.time(); loop(); time.sleep(max(0, args.interval - (time.time() - t)))
except KeyboardInterrupt: pass
finally: 
    if 'whisplay' in locals(): whisplay.cleanup()
    sys_deinit()
