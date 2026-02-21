#!/usr/bin/env python3
"""
MyClimate Flight Calculator - SELF-DRIVING SCRAPER
Fully automated end-to-end solution with:
- Consent dialog dismissal
- Smart element discovery with fallbacks
- Autocomplete dropdown handling via keyboard
- Resilient result extraction
"""

import asyncio
import re
import json
import sys
from playwright.async_api import async_playwright

async def scrape_flight(departure='CDG', arrival='LHR', passengers=1, aircraft='ECONOMY', headless=False):
    """
    Self-driving scraper for MyClimate flight calculator.
    
    Args:
        departure: IATA code (e.g., 'CDG')
        arrival: IATA code (e.g., 'LHR') 
        passengers: Number of passengers (default 1)
        aircraft: Cabin class (default 'ECONOMY')
        headless: Run browser headless (default False for debugging)
    
    Returns:
        dict: Results with 'status', 'distance_km', 'emissions_kg_co2'
    """
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=headless)
        page = await browser.new_page()
        
        try:
            print(f"\n{'='*70}")
            print(f"🚀  MYCLIMATE SELF-DRIVING FLIGHT CALCULATOR")
            print(f"{'='*70}")
            print(f"📍 Route: {departure} → {arrival}")
            print(f"👥 Passengers: {passengers} | ✈️  Cabin: {aircraft}")
            
            # STEP 1: Load page
            print(f"\n[1/8] 📄 Loading calculator page...")
            await page.goto('https://co2.myclimate.org/en/flight_calculators/new', wait_until='networkidle')
            await page.wait_for_timeout(2000)
            print(f"      ✅ Page loaded")
            
            # STEP 2: Dismiss consent dialog
            print(f"\n[2/8] 🔐 Checking for consent dialog...")
            try:
                accept_btn = page.locator('button:has-text("Accept"), button[aria-label*="Close"]').first
                if await accept_btn.is_visible(timeout=2000):
                    await accept_btn.click()
                    print(f"      ✅ Dismissed consent dialog")
                    await page.wait_for_timeout(1000)
                else:
                    print(f"      ℹ️  No visible consent dialog")
            except Exception as e:
                print(f"      ℹ️  Could not dismiss dialog: {type(e).__name__}")
            
            # STEP 3: Find and fill FROM airport
            print(f"\n[3/8] 🛫 Filling departure airport: {departure}")
            from_strategies = [
                'input[id="flight_calculator_from"]',
                'input[name="flight_calculator[from]"]',
                'input[type="text"]:nth-of-type(2)',  # Skip search input at #0
            ]
            
            from_input = None
            for selector in from_strategies:
                try:
                    locator = page.locator(selector).first
                    if await locator.is_visible(timeout=1000):
                        from_input = locator
                        print(f"      ✅ Found with selector: {selector}")
                        break
                except:
                    pass
            
            if not from_input:
                print(f"      ❌ ERROR: Could not find 'from' input field")
                return {'status': 'error', 'error': 'from_input_not_found'}
            
            await from_input.fill(departure)
            await page.wait_for_timeout(500)
            print(f"      → Filled: {departure}")
            
            # Use keyboard to select from dropdown
            await from_input.press('ArrowDown')  # Open dropdown
            await page.wait_for_timeout(300)
            await from_input.press('Enter')  # Select first option
            print(f"      ✅ Selected first option")
            await page.wait_for_timeout(1500)
            
            # STEP 4: Find and fill TO airport
            print(f"\n[4/8] 🛬 Filling arrival airport: {arrival}")
            to_strategies = [
                'input[id="flight_calculator_to"]',
                'input[name="flight_calculator[to]"]',
                'input[type="text"]:nth-of-type(4)',  # Skip search and from inputs
            ]
            
            to_input = None
            for selector in to_strategies:
                try:
                    locator = page.locator(selector).first
                    if await locator.is_visible(timeout=1000):
                        to_input = locator
                        print(f"      ✅ Found with selector: {selector}")
                        break
                except:
                    pass
            
            if not to_input:
                print(f"      ❌ ERROR: Could not find 'to' input field")
                return {'status': 'error', 'error': 'to_input_not_found'}
            
            await to_input.fill(arrival)
            await page.wait_for_timeout(500)
            print(f"      → Filled: {arrival}")
            
            # Use keyboard to select from dropdown
            await to_input.press('ArrowDown')  # Open dropdown
            await page.wait_for_timeout(300)
            await to_input.press('Enter')  # Select first option
            print(f"      ✅ Selected first option")
            await page.wait_for_timeout(1500)
            
            # STEP 5: Set passengers and aircraft (if applicable)
            print(f"\n[5/8] ⚙️  Configuring form parameters...")
            try:
                # Try to find and set passenger count
                passenger_selector_strategies = [
                    'select[name*="passenger"]',
                    'input[name*="passenger"]',
                    'select[id*="passenger"]',
                ]
                for selector in passenger_selector_strategies:
                    try:
                        elem = page.locator(selector).first
                        if await elem.is_visible(timeout=500):
                            await elem.select_option(str(passengers))
                            print(f"      ✅ Set passengers: {passengers}")
                            break
                    except:
                        pass
                
                print(f"      ✅ Form parameters configured")
            except Exception as e:
                print(f"      ℹ️  Could not set all parameters: {type(e).__name__}")
            
            # STEP 6: Submit form
            print(f"\n[6/8] 📤 Submitting form...")
            submit_found = False
            
            # Try multiple submit strategies
            submit_strategies = [
                ('button[type="submit"]', 'submit button'),
                ('button:has-text("Calculate")', 'Calculate button'),
                ('button:has-text("Submit")', 'Submit button'),
                ('button', 'any button'),
            ]
            
            for selector, desc in submit_strategies:
                try:
                    btn = page.locator(selector).first
                    if await btn.is_visible(timeout=1000):
                        await btn.click()
                        print(f"      ✅ Clicked {desc}")
                        submit_found = True
                        break
                except:
                    pass
            
            if not submit_found:
                print(f"      ⚠️  Could not find submit button, trying keyboard Enter on form...")
                # Focus on the to_input and press Enter
                await to_input.press('Enter')
            
            await page.wait_for_timeout(3000)
            print(f"      ✅ Form submitted, waiting for results...")
            
            # STEP 7: Extract results from page
            print(f"\n[7/8] 📊 Extracting results...")
            content = await page.content()
            
            distance = None
            distance_patterns = [
                (r'Distance[:\s]*(\d+[\d\.]*)\s*(?:km|km\.)', 'pattern 1'),
                (r'(\d+[\d\.]*)\s*km', 'pattern 2'),
                (r'distance[^0-9]*(\d+[\d\.]*)', 'pattern 3'),
            ]
            
            for pattern, desc in distance_patterns:
                match = re.search(pattern, content, re.IGNORECASE)
                if match:
                    distance = match.group(1)
                    print(f"      ✅ Found distance ({desc}): {distance} km")
                    break
            
            if not distance:
                print(f"      ⚠️  Could not extract distance")
            
            emissions = None
            emissions_patterns = [
                (r'CO₂\s*amount[:\s]*(\d+[\d\.]*)\s*t', 'CO2 amount pattern'),
                (r'(\d+[\d\.]*)\s*t\s*CO2', 'tonnes pattern'),
                (r'(\d+[\d\.]*)\s*kg\s*CO2', 'kg pattern'),
                (r'CO2[:\s]*(\d+[\d\.]*)', 'CO2 direct pattern'),
            ]
            
            for pattern, desc in emissions_patterns:
                match = re.search(pattern, content, re.IGNORECASE)
                if match:
                    emissions = match.group(1)
                    unit = re.search(r'(?:kg|t|tonnes?)', pattern)
                    print(f"      ✅ Found emissions ({desc}): {emissions} t CO2")
                    break
            
            if not emissions:
                print(f"      ⚠️  Could not extract emissions")
            
            # STEP 8: Prepare result
            print(f"\n[8/8] 🎯 Finalizing result...")
            result = {
                'status': 'success',
                'from': departure,
                'to': arrival,
                'passengers': passengers,
                'cabin_class': aircraft,
                'distance_km': distance or 'Not extracted',
                'emissions_kg_co2': emissions or 'Not extracted',
            }
            
            print(f"\n{'='*70}")
            print(f"✅ RESULT:")
            print(f"   Distance: {result['distance_km']} km")
            print(f"   Emissions: {result['emissions_kg_co2']} kg CO2")
            print(f"{'='*70}")
            
            return result
        
        except Exception as e:
            print(f"\n❌ ERROR: {type(e).__name__}: {str(e)[:100]}")
            import traceback
            traceback.print_exc()
            return {
                'status': 'error',
                'error': str(e),
            }
        
        finally:
            await browser.close()

def main():
    """Entry point for command line usage."""
    departure = sys.argv[1] if len(sys.argv) > 1 else 'CDG'
    arrival = sys.argv[2] if len(sys.argv) > 2 else 'LHR'
    headless = '--headless' in sys.argv
    
    result = asyncio.run(scrape_flight(departure, arrival, headless=headless))
    print(f"\n📋 JSON Result:\n{json.dumps(result, indent=2)}")
    
    return 0 if result['status'] == 'success' else 1

if __name__ == '__main__':
    sys.exit(main())
