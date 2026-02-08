# Smart Scrape Enhancements

This document details the enhancements made to the `smart_scrape` MCP tool to improve its robustness, accuracy, and compatibility with modern web technologies and LLM outputs.

## 1. HTML Cleaning & Data Attributes
- **Preservation of `data-*` Attributes:** The HTML cleaning logic has been updated to preserve critical data attributes such as `data-ref`, `data-value`, `data-field`, and `data-symbol`. This allows the LLM to identify data points that are stored in attributes rather than text content.
- **Custom Tag Support:** The LLM prompt now explicitly instructs the model to look for custom HTML tags and attributes, improving extraction from modern frameworks (React, Vue, etc.).

## 2. Robust JSON Parsing
- **Comment Stripping:** The JSON parser now pre-processes the LLM response to strip `//` style comments, which are invalid JSON but commonly added by LLMs.
- **Flexible Schema Handling:**
  - Supports both `extractions` and `extraction_instructions` keys.
  - Handles extraction values as:
    - Simple Strings: `"key": "regex"`
    - Arrays of Strings: `"key": ["regex"]` (uses first element)
    - Arrays of Objects: `"key": [{"regex": "..."}]` (uses `regex` or `pattern` field)
  - This flexibility prevents crashes when the LLM deviates from the strict schema.

## 3. Automatic Regex Sanitization
- **Go Regex Compatibility:** The Go `regexp` engine (RE2) does not support lookarounds (`(?=...)`, `(?<=...)`).
- **Auto-Fix Logic:** The system now automatically sanitizes regex patterns containing lookarounds by converting them to non-capturing groups `(?:...)`. This preserves the logical structure needed for extraction (using capturing groups) while making the regex compile successfully in Go.

## 4. UI Output Improvements
- **Increased Truncation Limit:** The `AgentExecutor` result truncation limit has been increased from 5KB to 5MB.
- **Raw Results Visibility:** The logic that hid raw scraping results whenever a summary was generated has been removed. Raw results are now always returned to the UI unless they exceed the 5MB size limit.

## Limitations
- **Regex Syntax:** While lookarounds are auto-sanitized, extremely complex regex features specific to PCRE/Perl might still fail. Adhering to RE2 syntax is recommended.
- **LLM Context:** The HTML snapshot passed to the LLM is truncated to ~120k characters to fit context windows. Very large pages might lose footer content.
