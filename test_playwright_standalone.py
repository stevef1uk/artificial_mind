#!/usr/bin/env python3
"""
Standalone Playwright Python Test Program
==========================================
This script tests Playwright functionality in a clean Python environment.
It demonstrates basic browser automation capabilities.

Requirements:
    pip install playwright
    playwright install chromium
"""

import sys
import json
import asyncio
from playwright.async_api import async_playwright, TimeoutError as PlaywrightTimeoutError


async def test_basic_navigation(url: str = "https://example.com"):
    """Test basic page navigation and content extraction"""
    print(f"🌐 Testing basic navigation to {url}")
    
    async with async_playwright() as p:
        # Launch browser
        print("🚀 Launching Chromium browser...")
        browser = await p.chromium.launch(headless=True)
        
        # Create a new page
        page = await browser.new_page()
        
        try:
            # Navigate to URL
            print(f"📍 Navigating to {url}")
            await page.goto(url, wait_until="networkidle", timeout=30000)
            
            # Get page title
            title = await page.title()
            print(f"📄 Page Title: {title}")
            
            # Get page content
            content = await page.content()
            print(f"📝 Page Content Length: {len(content)} characters")
            
            # Extract text content
            text_content = await page.evaluate("() => document.body.innerText")
            print(f"📃 Text Content (first 200 chars):\n{text_content[:200]}...")
            
            # Take a screenshot
            await page.screenshot(path="/tmp/playwright_test_screenshot.png")
            print("📸 Screenshot saved to /tmp/playwright_test_screenshot.png")
            
            result = {
                "success": True,
                "url": page.url,
                "title": title,
                "content_length": len(content),
                "text_length": len(text_content),
            }
            
            return result
            
        except PlaywrightTimeoutError as e:
            print(f"⏱️ Timeout Error: {e}")
            return {"success": False, "error": f"Timeout: {e}"}
        except Exception as e:
            print(f"❌ Error: {e}")
            return {"success": False, "error": str(e)}
        finally:
            await page.close()
            await browser.close()
            print("✅ Browser closed")


async def test_interactive_actions(url: str = "https://www.google.com"):
    """Test interactive browser actions like clicking and filling forms"""
    print(f"\n🎯 Testing interactive actions on {url}")
    
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=True)
        page = await browser.new_page()
        
        try:
            # Navigate
            print(f"📍 Navigating to {url}")
            await page.goto(url, wait_until="networkidle", timeout=30000)
            
            # Wait for search box (Google's search input)
            print("🔍 Looking for search input...")
            search_input = await page.query_selector('input[name="q"], textarea[name="q"]')
            
            if search_input:
                print("✅ Found search input")
                
                # Fill the search box
                await search_input.fill("Playwright Python")
                print("⌨️ Filled search box with 'Playwright Python'")
                
                # Press Enter to search
                await search_input.press("Enter")
                print("↩️ Pressed Enter")
                
                # Wait for navigation
                await page.wait_for_load_state("networkidle", timeout=10000)
                
                # Get new page title
                title = await page.title()
                print(f"📄 New Page Title: {title}")
                
                result = {
                    "success": True,
                    "action": "search",
                    "title": title,
                    "url": page.url,
                }
            else:
                print("⚠️ Search input not found")
                result = {
                    "success": False,
                    "error": "Search input not found"
                }
            
            return result
            
        except Exception as e:
            print(f"❌ Error: {e}")
            return {"success": False, "error": str(e)}
        finally:
            await page.close()
            await browser.close()
            print("✅ Browser closed")


async def test_complex_selectors(url: str = "https://news.ycombinator.com"):
    """Test complex CSS selectors and data extraction"""
    print(f"\n🎨 Testing complex selectors on {url}")
    
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=True)
        page = await browser.new_page()
        
        try:
            # Navigate
            print(f"📍 Navigating to {url}")
            await page.goto(url, wait_until="networkidle", timeout=30000)
            
            # Extract news titles
            print("📰 Extracting news titles...")
            titles = await page.evaluate("""
                () => {
                    const titleElements = document.querySelectorAll('.titleline > a');
                    return Array.from(titleElements).slice(0, 5).map(el => ({
                        text: el.innerText,
                        href: el.href
                    }));
                }
            """)
            
            print(f"✅ Found {len(titles)} news items:")
            for i, item in enumerate(titles, 1):
                print(f"  {i}. {item['text']}")
                print(f"     → {item['href']}")
            
            result = {
                "success": True,
                "items_found": len(titles),
                "items": titles,
            }
            
            return result
            
        except Exception as e:
            print(f"❌ Error: {e}")
            return {"success": False, "error": str(e)}
        finally:
            await page.close()
            await browser.close()
            print("✅ Browser closed")


async def test_custom_url_with_config(url: str, operations: list):
    """
    Test custom URL with Playwright operations
    
    Args:
        url: Target URL
        operations: List of operations like:
            [
                {"type": "goto", "url": "https://example.com"},
                {"type": "click", "selector": "a.link"},
                {"type": "fill", "selector": "input[name='search']", "value": "test"},
                {"type": "wait", "selector": ".results"},
                {"type": "extract", "selector": "h1", "attribute": "innerText"}
            ]
    """
    print(f"\n🔧 Testing custom operations on {url}")
    
    async with async_playwright() as p:
        browser = await p.chromium.launch(headless=True)
        page = await browser.new_page()
        
        extracted_data = {}
        
        try:
            # Navigate to initial URL
            await page.goto(url, wait_until="networkidle", timeout=30000)
            print(f"✅ Navigated to {url}")
            
            # Execute operations
            for i, op in enumerate(operations, 1):
                op_type = op.get("type")
                print(f"  [{i}] Executing: {op_type}")
                
                if op_type == "goto":
                    target_url = op.get("url", url)
                    await page.goto(target_url, wait_until="networkidle")
                    print(f"      → Navigated to {target_url}")
                    
                elif op_type == "click":
                    selector = op.get("selector")
                    await page.click(selector, timeout=5000)
                    print(f"      → Clicked {selector}")
                    
                elif op_type == "fill":
                    selector = op.get("selector")
                    value = op.get("value")
                    await page.fill(selector, value, timeout=5000)
                    print(f"      → Filled {selector} with '{value}'")
                    
                elif op_type == "wait":
                    selector = op.get("selector")
                    timeout = op.get("timeout", 5000)
                    await page.wait_for_selector(selector, timeout=timeout)
                    print(f"      → Waited for {selector}")
                    
                elif op_type == "extract":
                    selector = op.get("selector")
                    attribute = op.get("attribute", "innerText")
                    key = op.get("key", f"extracted_{i}")
                    
                    if attribute == "innerText":
                        value = await page.evaluate(f"document.querySelector('{selector}')?.innerText")
                    elif attribute == "innerHTML":
                        value = await page.evaluate(f"document.querySelector('{selector}')?.innerHTML")
                    else:
                        value = await page.get_attribute(selector, attribute)
                    
                    extracted_data[key] = value
                    print(f"      → Extracted {key}: {str(value)[:100]}")
            
            # Get final page state
            title = await page.title()
            final_url = page.url
            
            result = {
                "success": True,
                "title": title,
                "url": final_url,
                "extracted": extracted_data,
            }
            
            return result
            
        except Exception as e:
            print(f"❌ Error during operation: {e}")
            return {
                "success": False,
                "error": str(e),
                "extracted": extracted_data,
            }
        finally:
            await page.close()
            await browser.close()
            print("✅ Browser closed")


def main():
    """Main entry point"""
    print("=" * 60)
    print("🎭 Playwright Python Standalone Test")
    print("=" * 60)
    
    # Parse command line arguments
    if len(sys.argv) > 1:
        command = sys.argv[1]
        
        if command == "basic":
            url = sys.argv[2] if len(sys.argv) > 2 else "https://example.com"
            result = asyncio.run(test_basic_navigation(url))
            
        elif command == "interactive":
            url = sys.argv[2] if len(sys.argv) > 2 else "https://www.google.com"
            result = asyncio.run(test_interactive_actions(url))
            
        elif command == "selectors":
            url = sys.argv[2] if len(sys.argv) > 2 else "https://news.ycombinator.com"
            result = asyncio.run(test_complex_selectors(url))
            
        elif command == "custom":
            if len(sys.argv) < 4:
                print("Usage: python test_playwright_standalone.py custom <url> <operations_json>")
                print("Example operations JSON:")
                print('[{"type": "click", "selector": "a.link"}]')
                sys.exit(1)
            
            url = sys.argv[2]
            operations = json.loads(sys.argv[3])
            result = asyncio.run(test_custom_url_with_config(url, operations))
            
        else:
            print(f"Unknown command: {command}")
            print("Available commands: basic, interactive, selectors, custom")
            sys.exit(1)
    else:
        # Run all tests
        print("\n🧪 Running all tests...\n")
        
        result1 = asyncio.run(test_basic_navigation())
        result2 = asyncio.run(test_interactive_actions())
        result3 = asyncio.run(test_complex_selectors())
        
        result = {
            "basic_navigation": result1,
            "interactive_actions": result2,
            "complex_selectors": result3,
        }
    
    # Print final result
    print("\n" + "=" * 60)
    print("📊 Final Result:")
    print(json.dumps(result, indent=2))
    print("=" * 60)
    
    # Return exit code based on success
    if isinstance(result, dict) and result.get("success") is False:
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

