# 🚀 Scraper Quick Start: "Easy Mode"

The **Scraper Planner** is our AI-powered tool that automatically generates scraping configurations. You don't need to write Playwright scripts or complex Regex by hand—just provide a URL and your goal.

## 🏃 The Easiest Way: One-Step Test
The `scripts/test_scrape_e2e.sh` script combines both steps. It uses the AI to plan the scrape and then executes it against the local service.

```bash
# Simply run the script to test Nationwide (default)
./scripts/test_scrape_e2e.sh
```

---

## 🛠️ Manual Step 1: Generate Configuration
If you want to create a custom scrape, use the `scrape-planner` tool. It examines the HTML and uses an LLM to generate the configuration.

```bash
# Example: Extract Nationwide savings products and rates
bin/scrape-planner -url "https://www.nationwide.co.uk/savings/cash-isas/" \
                   -goal "pull out all savings product names and their interest rates"
```

**What it does:**
1.  **Navigates** to the URL using Playwright.
2.  **Cleans** the DOM to fit into the LLM's context window.
3.  **Generates** a JSON configuration (saved to `/tmp/scrape_config.json`).
4.  **Self-Heals**: If you provide a previously working pattern (as a "hint") that is now broken due to website changes, the AI will automatically detect the failure, ignore the broken hint, and generate a new working extraction pattern based on the fresh HTML.

---

## 🚀 Step 2: Execute Scrape
Once you have a configuration, send it to the **Playwright Scraper Service**.

If you are running the end-to-end test script, it does this for you:
```bash
./scripts/test_scrape_e2e.sh
```

### Manual Execution (via curl):
```bash
# 1. Start a job using the generated config
JOB_ID=$(curl -s -X POST http://localhost:8080/scrape/start \
  -H 'Content-Type: application/json' \
  -d @/tmp/scrape_config.json | jq -r '.job_id')

# 2. Poll for results
curl "http://localhost:8080/scrape/job?job_id=$JOB_ID"
```

---

## 📊 Understanding Results
The scraper returns results in a structured format:

```json
{
  "status": "completed",
  "result": {
    "page_title": "Cash ISAs | Nationwide",
    "products": "5 Year Fixed Rate Cash ISA, 4.00\n1 Year Fixed Rate Cash ISA, 3.80...",
    "text_content": "..."
  }
}
```

- **Extractions**: Any field defined in your `extractions` object will appear in `result`.
- **Text Content**: The service always returns the full `innerText` of the page for debugging.
- **Auto-Cookie Handling**: The service automatically clicks common "Accept All" cookie buttons to reveal dynamic content.

---

## 💡 Tips for Success
1.  **Be specific with your goal**: Instead of "get data", use "extract a list of all product names and their percentage rates".
2.  **Complex Sites**: For sites with complex multi-step forms (like the EcoTree calculator), the Planner is a great starting point, but you may need to refine the `typescript_config` manually. See the [Configuration Guide](SCRAPER_CONFIGURATION_GUIDE.md) for advanced tips.
3.  **Local Testing**: Always test locally before deploying to the cluster using the `./scripts/test_scrape_e2e.sh` script.
