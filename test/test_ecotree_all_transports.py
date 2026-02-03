#!/usr/bin/env python3
"""
Test all three transport types on EcoTree CO2 calculator
- Plane: Southampton → Newcastle
- Train: Petersfield → London Waterloo
- Car: Portsmouth → London
"""

import asyncio
from playwright.async_api import async_playwright

async def test_plane():
    """Test Plane transport"""
    print("\n" + "=" * 60)
    print("🛫 TESTING PLANE: Southampton → Newcastle")
    print("=" * 60)
    
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=False, slow_mo=100)
        page = await browser.new_page()
        page.set_default_timeout(30000)
        
        try:
            await page.goto('https://ecotree.green/en/calculate-flight-co2')
            print("✅ Page loaded")
            
            await page.get_by_role('link', name='Plane').click()
            print("✅ Clicked 'Plane'")
            
            await page.get_by_role('textbox', name='From To Via').click()
            await page.get_by_role('textbox', name='From To Via').fill('southampton')
            print("✅ Filled 'Southampton'")
            
            await page.get_by_text('Southampton, United Kingdom').click()
            print("✅ Selected Southampton")
            
            await page.locator('input[name="To"]').click()
            await page.locator('input[name="To"]').fill('newcastle')
            print("✅ Filled 'Newcastle'")
            
            await page.get_by_text('Newcastle, United Kingdom, (').click()
            print("✅ Selected Newcastle")
            
            await page.get_by_role('link', name=' Calculate my emissions ').click()
            print("✅ Clicked Calculate")
            
            await page.wait_for_timeout(3000)
            
            # Extract results
            try:
                co2_text = await page.locator('text=/\\d+\\s*kg/').first.inner_text()
                distance_text = await page.locator('text=/\\d+\\s*km/').first.inner_text()
                print(f"\n📊 RESULTS:")
                print(f"   ✈️  CO2: {co2_text}")
                print(f"   📏 Distance: {distance_text}")
                return True
            except Exception as e:
                print(f"   ⚠️  Could not extract results: {e}")
                return False
                
        except Exception as e:
            print(f"❌ Error: {e}")
            return False
        finally:
            await browser.close()

async def test_train():
    """Test Train transport"""
    print("\n" + "=" * 60)
    print("🚂 TESTING TRAIN: Petersfield → London Waterloo")
    print("=" * 60)
    
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=False, slow_mo=100)
        page = await browser.new_page()
        page.set_default_timeout(30000)
        
        try:
            # Direct navigation to train page
            await page.goto('https://ecotree.green/en/calculate-train-co2')
            print("✅ Page loaded (direct URL)")
            
            await page.wait_for_timeout(2000)
            
            # Fill From field
            await page.locator('#geosuggest__input').first.fill('Petersfield')
            print("✅ Filled 'Petersfield'")
            
            await page.wait_for_timeout(1000)
            await page.get_by_text('Petersfield, UK', exact=True).click()
            print("✅ Selected Petersfield")
            
            await page.wait_for_timeout(1000)
            
            # Fill To field
            await page.locator('#geosuggest__input').nth(1).fill('London')
            print("✅ Filled 'London'")
            
            await page.wait_for_timeout(1000)
            await page.get_by_text('London, UK', exact=True).click()
            print("✅ Selected London")
            
            await page.wait_for_timeout(1000)
            await page.get_by_role('link', name=' Calculate my emissions ').click()
            print("✅ Clicked Calculate")
            
            await page.wait_for_timeout(3000)
            
            # Extract results
            try:
                co2_text = await page.locator('text=/\\d+\\s*kg/').first.inner_text()
                distance_text = await page.locator('text=/\\d+\\s*km/').first.inner_text()
                print(f"\n📊 RESULTS:")
                print(f"   🚂 CO2: {co2_text}")
                print(f"   📏 Distance: {distance_text}")
                return True
            except Exception as e:
                print(f"   ⚠️  Could not extract results: {e}")
                return False
                
        except Exception as e:
            print(f"❌ Error: {e}")
            await page.screenshot(path='/tmp/train_error.png')
            print("📸 Screenshot saved to /tmp/train_error.png")
            return False
        finally:
            await browser.close()

async def test_car():
    """Test Car transport"""
    print("\n" + "=" * 60)
    print("🚗 TESTING CAR: Portsmouth → London")
    print("=" * 60)
    
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=False, slow_mo=100)
        page = await browser.new_page()
        page.set_default_timeout(30000)
        
        try:
            # Direct navigation to car page
            await page.goto('https://ecotree.green/en/calculate-car-co2')
            print("✅ Page loaded (direct URL)")
            
            await page.wait_for_timeout(2000)
            
            # Fill From field
            await page.locator('#geosuggest__input').first.fill('Portsmouth')
            print("✅ Filled 'Portsmouth'")
            
            await page.wait_for_timeout(1000)
            await page.get_by_text('Portsmouth, UK').click()
            print("✅ Selected Portsmouth")
            
            await page.wait_for_timeout(1000)
            
            # Fill To field
            await page.locator('#geosuggest__input').nth(1).fill('London')
            print("✅ Filled 'London'")
            
            await page.wait_for_timeout(1000)
            await page.get_by_role('option', name='London, UK', exact=True).click()
            print("✅ Selected London")
            
            await page.wait_for_timeout(1000)
            await page.get_by_role('link', name=' Calculate my emissions ').click()
            print("✅ Clicked Calculate")
            
            await page.wait_for_timeout(3000)
            
            # Extract results
            try:
                co2_text = await page.locator('text=/\\d+\\s*kg/').first.inner_text()
                distance_text = await page.locator('text=/\\d+\\s*km/').first.inner_text()
                print(f"\n📊 RESULTS:")
                print(f"   🚗 CO2: {co2_text}")
                print(f"   📏 Distance: {distance_text}")
                return True
            except Exception as e:
                print(f"   ⚠️  Could not extract results: {e}")
                return False
                
        except Exception as e:
            print(f"❌ Error: {e}")
            await page.screenshot(path='/tmp/car_error.png')
            print("📸 Screenshot saved to /tmp/car_error.png")
            return False
        finally:
            await browser.close()

async def main():
    """Run all tests"""
    print("\n" + "🌍 " * 20)
    print("EcoTree CO2 Calculator - Testing All Transport Types")
    print("🌍 " * 20)
    
    results = {}
    
    # Test Plane
    results['plane'] = await test_plane()
    await asyncio.sleep(2)
    
    # Test Train
    results['train'] = await test_train()
    await asyncio.sleep(2)
    
    # Test Car
    results['car'] = await test_car()
    
    # Summary
    print("\n" + "=" * 60)
    print("📊 SUMMARY")
    print("=" * 60)
    print(f"✈️  Plane:  {'✅ PASS' if results['plane'] else '❌ FAIL'}")
    print(f"🚂 Train:  {'✅ PASS' if results['train'] else '❌ FAIL'}")
    print(f"🚗 Car:    {'✅ PASS' if results['car'] else '❌ FAIL'}")
    print("=" * 60)
    
    all_pass = all(results.values())
    if all_pass:
        print("🎉 All tests passed!")
    else:
        print("⚠️  Some tests failed")
    
    return all_pass

if __name__ == "__main__":
    success = asyncio.run(main())
    exit(0 if success else 1)

