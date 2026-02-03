#!/usr/bin/env python3
"""Debug script to see what's actually on the EcoTree results page"""

from playwright.sync_api import sync_playwright
import re

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page()
    
    print("🚀 Navigating to EcoTree...")
    page.goto('https://ecotree.green/en/calculate-flight-co2')
    
    print("✈️  Selecting Plane...")
    page.get_by_role('link', name='Plane').click()
    
    print("📍 Entering Southampton...")
    page.get_by_role('textbox', name='From To Via').click()
    page.get_by_role('textbox', name='From To Via').fill('southampton')
    page.get_by_text('Southampton, United Kingdom').click()
    
    print("📍 Entering Newcastle...")
    page.locator('input[name="To"]').click()
    page.locator('input[name="To"]').fill('newcastle')
    page.wait_for_timeout(1000)
    page.get_by_text('Newcastle').first.click()
    
    print("🔢 Clicking Calculate...")
    page.get_by_role('link', name=' Calculate my emissions ').click()
    
    print("⏳ Waiting for results...")
    page.wait_for_timeout(5000)
    
    print(f"\n📊 Current URL: {page.url}")
    
    # Get all text content
    content = page.text_content('body')
    
    # Find all numbers followed by kg
    kg_matches = re.findall(r'(\d+(?:[.,]\d+)?)\s*kg', content, re.IGNORECASE)
    print(f"\n🔍 All 'kg' values found: {kg_matches}")
    
    # Find all numbers followed by km
    km_matches = re.findall(r'(\d+(?:[.,]\d+)?)\s*km', content, re.IGNORECASE)
    print(f"🔍 All 'km' values found: {km_matches}")
    
    # Try to find the specific result elements
    print("\n🎯 Looking for result elements...")
    
    # Take a screenshot
    page.screenshot(path='/tmp/ecotree_result.png')
    print("📸 Screenshot saved to /tmp/ecotree_result.png")
    
    browser.close()

