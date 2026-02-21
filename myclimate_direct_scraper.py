#!/usr/bin/env python3
"""
Direct MyClimate Flight Calculator Scraper
Bypasses LLM complexity with smart element discovery
"""
import asyncio
from playwright.async_api import async_playwright

async def scrape_myclimate():
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=True)
        page = await browser.new_page()
        
        # Navigate to page
        print("📍 Navigating to MyClimate...")
        await page.goto('https://co2.myclimate.org/en/flight_calculators/new', wait_until='networkidle')
        await page.wait_for_timeout(2000)
        
        # Strategies for element discovery - try multiple patterns
        async def fill_field_smart(field_name, value):
            """Use multiple strategies to find and fill a field"""
            selectors = [
                f'input[placeholder*="{field_name}"]',
                f'input[id*="{field_name.lower()}"]',
                f'input[name*="{field_name.lower()}"]',
                f'label:has-text("{field_name}") ~ input',
                f'input',  # Last resort: try first input
            ]
            
            for selector in selectors:
                try:
                    element = page.locator(selector).first
                    visible = await element.is_visible(timeout=1000)
                    if visible:
                        print(f"✅ Found {field_name} with: {selector}")
                        await element.fill(value)
                        await page.wait_for_timeout(1000)
                        return True
                except:
                    pass
            return False
        
        # Try filling From airport
        print("✈️  Filling flight details...")
        if await fill_field_smart("From", "CDG"):
            # Wait for dropdown and click first option
            try:
                await page.wait_for_selector("li", timeout=2000)
                first_option = page.locator("li").first
                await first_option.click()
                await page.wait_for_timeout(500)
            except:
                print("⚠️  No dropdown found, continuing...")
        
        # Fill To airport
        if await fill_field_smart("To", "LHR"):
            try:
                await page.wait_for_selector("li", timeout=2000)
                first_option = page.locator("li").first
                await first_option.click()
                await page.wait_for_timeout(500)
            except:
                pass
        
        # Try to select aircraft and passengers via any available method
        print("🎯 Selecting form options...")
        try:
            # Try selectOption for any select elements
            selects = await page.query_selector_all("select")
            for i, select in enumerate(selects):
                if i == 0:
                    await page.select_option(f"select:nth-of-type({i+1})", "BOEING_737") if await page.query_selector(f"select:nth-of-type({i+1})") else None
                elif i == 1:
                    await page.select_option(f"select:nth-of-type({i+1})", "1") if await page.query_selector(f"select:nth-of-type({i+1})") else None
        except Exception as e:
            print(f"⚠️  Form selection: {e}")
        
        # Click Calculate button - try multiple patterns
        print("🔍 Clicking Calculate button...")
        button_selectors = [
            'button:has-text("Calculate")',
            'button:has-text("Submit")',
            'button[type="submit"]',
            'button:visible',
        ]
        
        for selector in button_selectors:
            try:
                btn = page.locator(selector).first
                if await btn.is_visible(timeout=500):
                    await btn.click()
                    print(f"✅ Clicked button: {selector}")
                    await page.wait_for_timeout(3000)
                    break
            except:
                pass
        
        # Extract results - multiple strategies
        print("📊 Extracting results...")
        try:
            # Get page content and search for result patterns
            content = await page.content()
            
            # Try to find distance
            import re
            distance_match = re.search(r'(\d+[\d\.]*)\s*km', content, re.IGNORECASE)
            distance = distance_match.group(1) + " km" if distance_match else "Not found"
            
            # Try to find emissions
            emissions_match = re.search(r'(\d+[\d\.]*)\s*(?:kg|t)\b', content, re.IGNORECASE)
            emissions = emissions_match.group(1) + " kg/t" if emissions_match else "Not found"
            
            print(f"📈 Distance: {distance}")
            print(f"📈 Emissions: {emissions}")
            
            return {"distance": distance, "emissions": emissions}
        except Exception as e:
            print(f"❌ Extraction failed: {e}")
        
        finally:
            await browser.close()

if __name__ == "__main__":
    result = asyncio.run(scrape_myclimate())
    print(f"\n✅ Final Result: {result}")
