# Headless Browser Tool

A headless browser tool built with [Playwright](https://github.com/playwright-community/playwright-go) that can navigate websites, fill forms, click buttons, and extract data. This tool is integrated as an MCP tool and can be used by the LLM to interact with dynamic web pages.

## Features

- Navigate to any URL
- Fill form fields (text inputs, textareas)
- Click buttons and links
- Select dropdown options
- Wait for elements to appear
- Extract data from pages (DOM-based and OCR fallback)
- Screenshot capture for debugging
- OCR-based text extraction when DOM methods fail
- Returns structured JSON data

## Building

```bash
cd tools/headless_browser
go mod tidy
go build -o ../../bin/headless-browser .
```

Or use the Makefile:

```bash
make build-tools
```

## Usage

### Command Line

```bash
./bin/headless-browser \
  -url "https://example.com" \
  -actions '[{"type":"fill","selector":"input[name=\"email\"]","value":"test@example.com"}]' \
  -timeout 30
```

### Playwright Install

The tool will attempt to install Playwright (driver + Chromium) automatically if missing. If you prefer to install manually:

```bash
cd tools/headless_browser
go run github.com/playwright-community/playwright-go/cmd/playwright install --with-deps chromium
```

### OCR Support (Optional)

The tool includes OCR (Optical Character Recognition) as a fallback for data extraction when DOM-based methods fail. OCR uses Tesseract and is automatically used when:

- Text extraction via selectors fails
- JavaScript-based extraction returns empty
- The field name or selector contains "co2" or "kg"

To enable OCR, install Tesseract OCR:

```bash
# Debian/Ubuntu
sudo apt-get install tesseract-ocr

# macOS
brew install tesseract

# Or use the Docker image which includes Tesseract
```

If Tesseract is not installed, the tool will continue to work but OCR extraction will be skipped with a warning message.

### Actions

Actions are JSON arrays where each action has:

- `type`: One of `fill`, `click`, `wait`, `select`, `extract`
- `selector`: CSS selector or XPath to find the element
- `value`: Value to fill (for `fill` and `select` actions)
- `extract`: Map of field names to selectors (for `extract` action)
- `wait_for`: Selector to wait for (for `wait` action)
- `timeout`: Timeout in seconds for this specific action

### Example: CO2 Calculator

```json
{
  "url": "https://ecotree.green/en/calculate-train-co2",
  "actions": [
    {
      "type": "wait",
      "selector": "body",
      "timeout": 10
    },
    {
      "type": "fill",
      "selector": "input[name=\"from\"]",
      "value": "Paris"
    },
    {
      "type": "fill",
      "selector": "input[name=\"to\"]",
      "value": "London"
    },
    {
      "type": "select",
      "selector": "select[name=\"transport_type\"]",
      "value": "train"
    },
    {
      "type": "click",
      "selector": "button[type=\"submit\"]"
    },
    {
      "type": "wait",
      "wait_for": ".result",
      "timeout": 10
    },
    {
      "type": "extract",
      "extract": {
        "co2_emissions": ".co2-result",
        "distance": ".distance"
      }
    }
  ],
  "timeout": 30
}
```

## MCP Integration

The tool is available as an MCP tool called `browse_web`. You can use it via the MCP API:

```json
{
  "method": "tools/call",
  "params": {
    "name": "browse_web",
    "arguments": {
      "url": "https://ecotree.green/en/calculate-train-co2",
      "actions": [
        {
          "type": "fill",
          "selector": "input[name=\"from\"]",
          "value": "Paris"
        },
        {
          "type": "fill",
          "selector": "input[name=\"to\"]",
          "value": "London"
        },
        {
          "type": "click",
          "selector": "button[type=\"submit\"]"
        },
        {
          "type": "extract",
          "extract": {
            "co2_emissions": ".result"
          }
        }
      ]
    }
  }
}
```

## Finding Selectors

To find the correct selectors for a form:

1. Open the website in a browser
2. Right-click on the form field and select "Inspect"
3. Look for the `id`, `name`, or `class` attributes
4. Use CSS selectors like:
   - `#id` for element ID
   - `input[name="fieldname"]` for name attribute
   - `.classname` for class
   - `button[type="submit"]` for attributes

## LLM Assistance

The tool can optionally use the LLM to help determine form selectors. When `use_llm_for_selectors` is set to `true`, the tool will:

1. First scrape the page HTML
2. Send it to the LLM with a description of what fields need to be filled
3. The LLM will suggest selectors
4. The tool will use those selectors to fill the form

This is useful when form structure is complex or dynamic.

## Limitations

- Requires Chrome/Chromium to be installed (rod uses Chrome DevTools Protocol)
- Some sites may detect headless browsers and block access
- Complex JavaScript-heavy sites may require additional wait times
- Selectors may break if the site structure changes

## Troubleshooting

- **Binary not found**: Run `make build-tools` to build all tools
- **Chrome not found**: Install Chrome or Chromium. rod will download it automatically if possible
- **Timeout errors**: Increase the timeout value or add explicit wait actions
- **Selector not found**: Inspect the page HTML to find the correct selector

