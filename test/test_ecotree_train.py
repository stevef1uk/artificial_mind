#!/usr/bin/env python3
"""
Standalone Playwright test for EcoTree Train CO2 calculator
Route: Petersfield → London Waterloo
"""

import asyncio
from playwright.async_api import async_playwright

async def test_ecotree_train():
    print("🚂 Testing EcoTree Train CO2 Calculator")
    print("=" * 60)
    print("Route: Petersfield → London Waterloo")
    print()

    async with async_playwright() as p:
        # Launch browser
        print("🌐 Launching browser...")
        browser = await p.chromium.launch(headless=False, slow_mo=500)  # Visible + slow for debugging
        page = await browser.new_page()
        
        # Set longer timeout for debugging
        page.set_default_timeout(60000)  # 60 seconds
        
        try:
            # Navigate to the page
            print("📍 Navigating to EcoTree...")
            await page.goto('https://ecotree.green/en/calculate-flight-co2')
            print("✅ Page loaded")
            
            # Click on Train tab
            print("\n🚂 Clicking 'Train' tab...")
            await page.get_by_role('link', name='Train').click()
            print("✅ Clicked 'Train'")
            
            # Wait and see what happens
            print("\n⏳ Waiting 3 seconds for form to load...")
            await page.wait_for_timeout(3000)
            
            # Take a screenshot
            await page.screenshot(path='/tmp/ecotree_train_after_click.png')
            print("📸 Screenshot saved to /tmp/ecotree_train_after_click.png")
            
            # Try to inspect the form
            print("\n🔍 Inspecting form elements...")
            
            # Check for textbox with "From To Via"
            try:
                textbox = page.get_by_role('textbox', name='From To Via')
                is_visible = await textbox.is_visible()
                print(f"   'From To Via' textbox visible: {is_visible}")
            except Exception as e:
                print(f"   ❌ 'From To Via' textbox not found: {e}")
            
            # Check for input[name="From"]
            try:
                from_input = page.locator('input[name="From"]')
                count = await from_input.count()
                if count > 0:
                    is_visible = await from_input.first.is_visible()
                    placeholder = await from_input.first.get_attribute('placeholder')
                    print(f"   input[name='From'] found: {count} elements, visible: {is_visible}, placeholder: {placeholder}")
                else:
                    print(f"   input[name='From'] not found")
            except Exception as e:
                print(f"   ❌ Error checking input[name='From']: {e}")
            
            # Check for any visible input elements
            try:
                all_inputs = page.locator('input[type="text"]:visible')
                count = await all_inputs.count()
                print(f"   Total visible text inputs: {count}")
                for i in range(min(count, 5)):  # Show first 5
                    input_elem = all_inputs.nth(i)
                    name = await input_elem.get_attribute('name')
                    placeholder = await input_elem.get_attribute('placeholder')
                    print(f"     [{i}] name='{name}', placeholder='{placeholder}'")
            except Exception as e:
                print(f"   ❌ Error listing inputs: {e}")
            
            print("\n" + "=" * 60)
            print("🔍 Now attempting to fill the form...")
            print("=" * 60)
            
            # Try METHOD 1: Using role-based selector
            print("\n📝 Method 1: Using getByRole textbox...")
            try:
                await page.get_by_role('textbox', name='From To Via').click()
                print("   ✅ Clicked textbox")
                await page.get_by_role('textbox', name='From To Via').fill('Petersfield')
                print("   ✅ Filled 'Petersfield'")
                await page.wait_for_timeout(1000)
                await page.get_by_text('Petersfield').click()
                print("   ✅ Selected 'Petersfield' from dropdown")
            except Exception as e:
                print(f"   ❌ Method 1 failed: {e}")
                
                # Try METHOD 2: Direct input selector
                print("\n📝 Method 2: Using input[name='From']...")
                try:
                    await page.locator('input[name="From"]').fill('Petersfield')
                    print("   ✅ Filled 'Petersfield'")
                    await page.wait_for_timeout(1000)
                    await page.get_by_text('Petersfield').click()
                    print("   ✅ Selected 'Petersfield' from dropdown")
                except Exception as e2:
                    print(f"   ❌ Method 2 failed: {e2}")
                    
                    # Try METHOD 3: Wait for selector first
                    print("\n📝 Method 3: Wait for selector then fill...")
                    try:
                        await page.wait_for_selector('input[type="text"]:visible', timeout=10000)
                        first_input = page.locator('input[type="text"]:visible').first
                        await first_input.fill('Petersfield')
                        print("   ✅ Filled 'Petersfield' in first visible input")
                        await page.wait_for_timeout(1000)
                        await page.get_by_text('Petersfield').click()
                        print("   ✅ Selected 'Petersfield' from dropdown")
                    except Exception as e3:
                        print(f"   ❌ Method 3 failed: {e3}")
                        raise
            
            # Fill destination
            print("\n📝 Filling destination (London Waterloo)...")
            await page.locator('input[name="To"]').fill('London Waterloo')
            print("   ✅ Filled 'London Waterloo'")
            await page.wait_for_timeout(1000)
            
            # Click on the suggestion
            await page.get_by_text('Waterloo, London').click()
            print("   ✅ Selected 'Waterloo, London' from dropdown")
            
            # Click Calculate
            print("\n🧮 Clicking 'Calculate my emissions'...")
            await page.get_by_role('link', name='Calculate my emissions').click()
            await page.wait_for_timeout(3000)
            print("   ✅ Clicked calculate")
            
            # Extract results
            print("\n📊 Extracting results...")
            
            # Try to get CO2 emissions
            try:
                co2_text = await page.locator('text=/\\d+\\s*kg/').first.inner_text()
                print(f"   ✈️  CO2 Emissions: {co2_text}")
            except:
                print("   ⚠️  Could not extract CO2 emissions")
            
            # Try to get distance
            try:
                distance_text = await page.locator('text=/\\d+\\s*km/').first.inner_text()
                print(f"   📏 Distance: {distance_text}")
            except:
                print("   ⚠️  Could not extract distance")
            
            # Take final screenshot
            await page.screenshot(path='/tmp/ecotree_train_result.png')
            print("\n📸 Final screenshot saved to /tmp/ecotree_train_result.png")
            
            print("\n" + "=" * 60)
            print("✅ Test completed!")
            print("=" * 60)
            
            # Keep browser open for inspection
            print("\n⏸️  Browser will stay open for 10 seconds for inspection...")
            await page.wait_for_timeout(10000)
            
        except Exception as e:
            print(f"\n❌ Error: {e}")
            await page.screenshot(path='/tmp/ecotree_train_error.png')
            print("📸 Error screenshot saved to /tmp/ecotree_train_error.png")
            raise
        finally:
            await browser.close()

if __name__ == "__main__":
    asyncio.run(test_ecotree_train())

