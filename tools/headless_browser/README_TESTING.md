# Standalone Testing Guide

This document explains how to test the headless browser tool independently before integrating it into the core application. The main tool now uses Playwright; the standalone tests still demonstrate core flows and may be updated later.

## Running Tests

The standalone test program (`test_standalone.go`) provides three test scenarios:

### 1. Basic Navigation Test

Tests basic browser functionality:
```bash
cd tools/headless_browser
go run test_standalone.go basic
```

This test:
- Launches a headless browser
- Navigates to example.com
- Extracts page title and heading
- Verifies basic functionality

### 2. Form Filling Test

Tests form interaction:
```bash
go run test_standalone.go form
```

This test:
- Navigates to httpbin.org/forms/post
- Fills form fields (customer name, telephone)
- Selects radio buttons
- Verifies form values

### 3. CO2 Calculator Test

Tests the actual use case (ecotree.green):
```bash
go run test_standalone.go co2
```

This test:
- Navigates to the CO2 calculator
- Attempts to find form fields using multiple selector strategies
- Tries to fill "from", "to", and transport type fields
- Clicks the calculate button
- Extracts results
- Saves page HTML to `/tmp/ecotree_page.html` for inspection

## Understanding Test Results

### Success Indicators

✅ **Green checkmarks** indicate successful operations:
- `✅ Page title: ...` - Page loaded successfully
- `✅ Filled 'from' field` - Form field was found and filled
- `✅ Clicked calculate button` - Button was found and clicked

### Warning Indicators

⚠️ **Yellow warnings** indicate issues that don't fail the test:
- `⚠️ Could not find 'from' field` - Selector didn't match any elements
- `⚠️ No results extracted` - Results section not found (may be expected)

### Debugging

If tests fail or don't find elements:

1. **Check the saved HTML file**:
   ```bash
   cat /tmp/ecotree_page.html | grep -i "input\|button\|select" | head -20
   ```

2. **Inspect page structure**:
   The test outputs:
   - Number of input/select/button elements found
   - Page HTML snippet
   - Current URL (to detect redirects)

3. **Update selectors**:
   If the page structure is different, update the selectors in the test:
   ```go
   fromSelectors := []string{
       "input[name='from']",           // Try name attribute
       "input[placeholder*='From']",   // Try placeholder
       "input[id*='from']",            // Try ID
       "#your-actual-id",              // Add actual selectors found
   }
   ```

## Common Issues

### Page Not Loading

**Symptoms**: Found 0 elements of any kind

**Solutions**:
- Page may require cookies/consent - check for cookie banners
- Page may block headless browsers - try non-headless mode
- Page may require more time to load - increase wait times

### Elements Not Found

**Symptoms**: `⚠️ Could not find 'from' field`

**Solutions**:
1. Inspect the saved HTML file to find actual selectors
2. The page may use React/Vue - elements are created dynamically
3. Increase wait times for JavaScript to execute
4. Try different selector strategies (ID, class, data attributes, XPath)

### Form Submission Fails

**Symptoms**: Button clicked but no results

**Solutions**:
- Check if form requires validation
- Verify all required fields are filled
- Check for error messages in the page
- Try waiting longer after clicking

## Next Steps

After standalone testing passes:

1. **Verify core functionality works** - All three tests should pass
2. **Identify correct selectors** - Use the saved HTML to find actual selectors
3. **Update main tool** - Update `main.go` with correct selectors if needed
4. **Test via MCP** - Test the tool through the MCP API
5. **Test with LLM** - Have the LLM use the tool naturally

## Example: Finding Correct Selectors

If the CO2 calculator test doesn't find elements:

1. Open the saved HTML:
   ```bash
   less /tmp/ecotree_page.html
   ```

2. Search for form elements:
   ```bash
   grep -i "from\|to\|train" /tmp/ecotree_page.html | head -20
   ```

3. Look for:
   - Input field IDs: `<input id="...">`
   - Input field names: `<input name="...">`
   - Button classes: `<button class="...">`
   - Data attributes: `<div data-testid="...">`

4. Update selectors in the test and try again

