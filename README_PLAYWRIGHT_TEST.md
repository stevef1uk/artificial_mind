# Playwright Python Standalone Test

This directory contains a standalone Python program to test Playwright functionality in a clean virtual environment.

## Overview

The test program (`test_playwright_standalone.py`) provides several test scenarios to verify Playwright works correctly:

1. **Basic Navigation** - Load a page and extract basic information
2. **Interactive Actions** - Fill forms, click buttons, and interact with elements
3. **Complex Selectors** - Use advanced CSS selectors to extract structured data
4. **Custom Operations** - Execute custom Playwright operations from JSON config

## Quick Start

### 1. Setup the Virtual Environment

```bash
chmod +x setup_playwright_venv.sh
./setup_playwright_venv.sh
```

This will:
- Create a Python virtual environment in `playwright_venv/`
- Install the `playwright` package
- Download Chromium browser for Playwright

### 2. Activate the Virtual Environment

```bash
source playwright_venv/bin/activate
```

### 3. Run Tests

Run all tests:
```bash
python test_playwright_standalone.py
```

Run specific tests:
```bash
# Basic navigation test
python test_playwright_standalone.py basic https://example.com

# Interactive actions (search on Google)
python test_playwright_standalone.py interactive

# Complex selectors (extract Hacker News titles)
python test_playwright_standalone.py selectors

# Custom operations with JSON config
python test_playwright_standalone.py custom "https://example.com" '[{"type":"click","selector":"a"}]'
```

## Test Descriptions

### Basic Navigation Test
- Navigates to a URL
- Extracts page title and content
- Takes a screenshot
- Returns basic page information

### Interactive Actions Test
- Opens Google
- Finds the search input box
- Fills it with a search query
- Presses Enter to search
- Waits for results page

### Complex Selectors Test
- Opens Hacker News
- Uses JavaScript evaluation to extract data
- Extracts top 5 news items with titles and links
- Demonstrates advanced DOM querying

### Custom Operations Test
- Accepts custom URL and operations list
- Supports operations:
  - `goto` - Navigate to URL
  - `click` - Click an element
  - `fill` - Fill a form field
  - `wait` - Wait for selector
  - `extract` - Extract data from page

Example operations JSON:
```json
[
  {"type": "goto", "url": "https://example.com"},
  {"type": "click", "selector": "a.link"},
  {"type": "fill", "selector": "input[name='search']", "value": "test"},
  {"type": "wait", "selector": ".results"},
  {"type": "extract", "selector": "h1", "attribute": "innerText", "key": "title"}
]
```

## Output

All tests output:
- Console logs showing progress
- Final JSON result with extracted data
- Screenshots saved to `/tmp/playwright_test_screenshot.png` (basic test only)

Example output:
```json
{
  "success": true,
  "url": "https://example.com",
  "title": "Example Domain",
  "content_length": 1256,
  "text_length": 452
}
```

## Troubleshooting

### Browser Installation Issues

If browsers don't install correctly:
```bash
source playwright_venv/bin/activate
playwright install --force chromium
```

### Python Version

Requires Python 3.8 or higher. Check your version:
```bash
python3 --version
```

### Dependencies

If you need to reinstall dependencies:
```bash
source playwright_venv/bin/activate
pip install --upgrade playwright
playwright install chromium
```

### Headless Mode

The tests run in headless mode by default. To see the browser in action, edit `test_playwright_standalone.py` and change:
```python
browser = await p.chromium.launch(headless=True)
```
to:
```python
browser = await p.chromium.launch(headless=False)
```

## Integration with Your Go Project

This Python test program can help verify that Playwright works correctly on your system. Once confirmed, you can:

1. Compare behavior with your Go Playwright implementation (`tools/headless_browser/`)
2. Use it as a reference for implementing similar operations in Go
3. Debug issues by testing the same operations in both Python and Go
4. Generate test cases that can be converted to Go code

## Comparison with Go Implementation

Your Go project uses:
- `github.com/playwright-community/playwright-go` package
- Headless browser binary at `tools/headless_browser/`
- TypeScript operation parser in `hdn/playwright/parser.go`

This Python implementation:
- Uses the official `playwright` Python package
- Provides async/await API (cleaner than Go's approach)
- Can be used for rapid prototyping and testing

## Next Steps

1. Run the tests to verify Playwright works
2. Compare results with your Go implementation
3. Use this as a reference for debugging scraping issues
4. Consider using Python for complex scraping scenarios where Go's API is cumbersome

## Deactivate Virtual Environment

When done:
```bash
deactivate
```

