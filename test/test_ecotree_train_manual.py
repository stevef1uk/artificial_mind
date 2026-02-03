#!/usr/bin/env python3
"""
Manual inspection test for EcoTree Train form
Leaves browser open so you can inspect the form structure
"""

import asyncio
from playwright.async_api import async_playwright

async def inspect_train_form():
    print("🚂 Inspecting EcoTree Train Form")
    print("=" * 60)
    
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=False)
        page = await browser.new_page()
        
        print("📍 Navigating to EcoTree...")
        await page.goto('https://ecotree.green/en/calculate-flight-co2')
        
        print("🚂 Clicking 'Train' tab...")
        await page.get_by_role('link', name='Train').click()
        
        print("\n⏳ Waiting 3 seconds...")
        await page.wait_for_timeout(3000)
        
        print("\n🔍 Checking ALL input elements:")
        inputs = await page.locator('input').all()
        for i, inp in enumerate(inputs):
            input_type = await inp.get_attribute('type')
            name = await inp.get_attribute('name')
            placeholder = await inp.get_attribute('placeholder')
            aria_label = await inp.get_attribute('aria-label')
            input_id = await inp.get_attribute('id')
            is_visible = await inp.is_visible()
            print(f"  Input [{i}]:")
            print(f"    type={input_type}, name={name}, id={input_id}")
            print(f"    placeholder={placeholder}")
            print(f"    aria-label={aria_label}")
            print(f"    visible={is_visible}")
            print()
        
        print("=" * 60)
        print("✅ Browser is open - you can manually inspect the page")
        print("   Press Ctrl+C when done")
        print("=" * 60)
        
        # Keep browser open indefinitely
        try:
            while True:
                await page.wait_for_timeout(60000)
        except KeyboardInterrupt:
            print("\n👋 Closing browser...")

if __name__ == "__main__":
    asyncio.run(inspect_train_form())

