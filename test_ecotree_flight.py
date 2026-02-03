#!/usr/bin/env python3
"""
Test EcoTree Flight CO2 Calculator
===================================
This script automates the EcoTree flight CO2 calculator to get emissions data.
Based on the TypeScript Playwright test script.
"""

import sys
import json
import asyncio
from playwright.async_api import async_playwright, TimeoutError as PlaywrightTimeoutError


async def calculate_flight_co2(from_city: str, to_city: str):
    """
    Calculate flight CO2 emissions using EcoTree calculator
    
    Args:
        from_city: Departure city (e.g., "southampton")
        to_city: Destination city (e.g., "newcastle")
    
    Returns:
        dict with success status and extracted data
    """
    print(f"🌍 Calculating CO2 emissions for flight: {from_city} → {to_city}")
    
    async with async_playwright() as p:
        # Launch browser
        print("🚀 Launching browser...")
        browser = await p.chromium.launch(headless=True)
        
        # Create a new page
        page = await browser.new_page()
        
        try:
            # Step 1: Navigate to the calculator page
            print("📍 Navigating to EcoTree calculator...")
            await page.goto('https://ecotree.green/en/calculate-flight-co2', 
                          wait_until="networkidle", 
                          timeout=30000)
            print(f"✅ Page loaded: {page.url}")
            
            # Step 2: Click on "Plane" tab (might already be selected, but click anyway)
            print("✈️  Selecting 'Plane' tab...")
            try:
                plane_link = page.get_by_role('link', name='Plane')
                await plane_link.click(timeout=5000)
                await page.wait_for_timeout(1000)  # Wait a bit for any animations
            except Exception as e:
                print(f"   ℹ️  Plane tab click skipped (might be default): {e}")
            
            # Step 3: Fill in the "From" field
            print(f"📝 Entering departure city: {from_city}")
            from_textbox = page.get_by_role('textbox', name='From To Via')
            await from_textbox.click(timeout=5000)
            await from_textbox.fill(from_city)
            await page.wait_for_timeout(1000)  # Wait for autocomplete
            
            # Step 4: Select the first autocomplete suggestion for departure
            print(f"🔍 Selecting autocomplete suggestion for {from_city}...")
            # Try to find and click the autocomplete option
            # The text might vary, so we'll try to click any visible option containing the city
            try:
                # Wait for autocomplete dropdown to appear
                await page.wait_for_selector('text=' + from_city.capitalize(), timeout=5000)
                # Click the first matching option (e.g., "Southampton, United Kingdom")
                await page.locator(f'text=/.*{from_city.capitalize()}.*/i').first.click()
                print(f"   ✅ Selected departure city")
            except Exception as e:
                print(f"   ⚠️  Autocomplete selection issue: {e}")
                # Try alternative approach - just press Enter
                await from_textbox.press('Enter')
            
            await page.wait_for_timeout(1000)
            
            # Step 5: Fill in the "To" field
            print(f"📝 Entering destination city: {to_city}")
            to_input = page.locator('input[name="To"]')
            await to_input.click(timeout=5000)
            await to_input.fill(to_city)
            await page.wait_for_timeout(1000)  # Wait for autocomplete
            
            # Step 6: Select the first autocomplete suggestion for destination
            print(f"🔍 Selecting autocomplete suggestion for {to_city}...")
            try:
                # Wait for autocomplete dropdown
                await page.wait_for_selector('text=' + to_city.capitalize(), timeout=5000)
                # Click the matching option
                await page.locator(f'text=/.*{to_city.capitalize()}.*/i').first.click()
                print(f"   ✅ Selected destination city")
            except Exception as e:
                print(f"   ⚠️  Autocomplete selection issue: {e}")
                # Try alternative approach
                await to_input.press('Enter')
            
            await page.wait_for_timeout(1000)
            
            # Step 7: Click the "Calculate my emissions" button
            print("🧮 Clicking calculate button...")
            calculate_button = page.get_by_role('link', name='Calculate my emissions')
            await calculate_button.click(timeout=5000)
            
            # Wait for results to load
            print("⏳ Waiting for results...")
            await page.wait_for_load_state("networkidle", timeout=15000)
            await page.wait_for_timeout(2000)  # Extra wait for any animations
            
            # Step 8: Extract the results
            print("📊 Extracting CO2 emissions data...")
            
            # Take a screenshot for debugging
            await page.screenshot(path="/tmp/ecotree_results.png")
            print("📸 Screenshot saved to /tmp/ecotree_results.png")
            
            # Extract page content to find CO2 values
            page_content = await page.content()
            
            # Try to extract specific CO2 values from the page
            # The exact selectors may vary based on the page structure
            co2_data = {}
            
            try:
                # Try to find CO2 emission text on the page
                # Look for text patterns like "X kg CO2" or "X tons CO2"
                text_content = await page.evaluate("() => document.body.innerText")
                
                # Parse for CO2 values (this is a simple approach)
                import re
                co2_matches = re.findall(r'([\d,.]+)\s*(kg|tons?|tonnes?)\s*(?:of\s+)?CO2', text_content, re.IGNORECASE)
                
                if co2_matches:
                    co2_data['co2_emissions'] = co2_matches
                    print(f"   ✅ Found CO2 values: {co2_matches}")
                
                # Store full text for analysis
                co2_data['page_text'] = text_content[:1000]  # First 1000 chars
                
            except Exception as e:
                print(f"   ⚠️  Error extracting CO2 data: {e}")
            
            # Get page title and URL
            title = await page.title()
            final_url = page.url
            
            result = {
                "success": True,
                "from_city": from_city,
                "to_city": to_city,
                "title": title,
                "url": final_url,
                "co2_data": co2_data,
            }
            
            return result
            
        except PlaywrightTimeoutError as e:
            print(f"⏱️ Timeout Error: {e}")
            return {
                "success": False,
                "error": f"Timeout: {e}",
                "from_city": from_city,
                "to_city": to_city,
            }
        except Exception as e:
            print(f"❌ Error: {e}")
            import traceback
            traceback.print_exc()
            return {
                "success": False,
                "error": str(e),
                "from_city": from_city,
                "to_city": to_city,
            }
        finally:
            await page.close()
            await browser.close()
            print("✅ Browser closed")


def main():
    """Main entry point"""
    print("=" * 60)
    print("🌱 EcoTree Flight CO2 Calculator Test")
    print("=" * 60)
    
    # Parse command line arguments
    from_city = "southampton"
    to_city = "newcastle"
    
    if len(sys.argv) > 1:
        from_city = sys.argv[1].lower()
    if len(sys.argv) > 2:
        to_city = sys.argv[2].lower()
    
    print(f"\n📍 Route: {from_city.upper()} → {to_city.upper()}\n")
    
    # Run the calculation
    result = asyncio.run(calculate_flight_co2(from_city, to_city))
    
    # Print final result
    print("\n" + "=" * 60)
    print("📊 Final Result:")
    print(json.dumps(result, indent=2))
    print("=" * 60)
    
    # Return exit code based on success
    if not result.get("success"):
        sys.exit(1)
    
    sys.exit(0)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\n⚠️ Interrupted by user")
        sys.exit(130)
    except Exception as e:
        print(f"\n❌ Fatal error: {e}")
        import traceback
        traceback.print_exc()
        sys.exit(1)

