# ✈️ Flight Search Service Architecture

## Overview
The Flight Search Service is a high-fidelity data extraction system designed to retrieve real-time flight information from Google Flights. It is built as an **MCP (Model Context Protocol)**-compatible tool that can be used by the reasoning engine to perform complex travel planning.

To ensure reliability on resource-constrained hardware (like Raspberry Pi) and to bypass complex browser protections, the service uses a distributed, multi-layered approach.

---

## 🏗️ System Architecture

The service consists of three primary layers:

### 1. Flight MCP Tool (`tools/flights`)
The entry point for the reasoning engine.
- **Role**: Orchestrates the search, handles configuration (language, region, currency), and performs the final data extraction.
- **Offloading Mode**: Detects the `SCRAPER_URL` environment variable. If set, it offloads the heavy browser work to the dedicated Scraper Service.
- **Native Mode**: (Fallback) Runs a local Playwright instance.

### 2. Playwright Scraper Service (`services/playwright_scraper`)
A standalone, high-performance browser automation service.
- **Role**: Executes complex, dynamic navigation scripts.
- **Capabilities**:
  - **Dynamic Scripting**: Supports the `FLIGHT_SCRAPER_SCRIPT` override for rapid iteration.
  - **Auto-Consent**: Automatically identifies and bypasses regional consent walls (EU/UK).
  - **Aesthetic Screenshots**: Captures high-resolution, full-page screenshots for visual verification and OCR.
  - **Resource Offloading**: Heavily optimized for K3s clusters, allowing the browser to run on high-performance nodes while the tool logic runs on the controller.

### 3. Extraction Engine (Hybrid OCR + LLM)
Once the scraper provides a screenshot and HTML, the tool uses a "Dual-Track" extraction strategy:
- **Track A: High-Fidelity OCR**: Uses Tesseract to extract raw text and price symbols from the screenshot. This is the most resilient method against DOM obfuscation.
- **Track B: SMART LLM Miner**: Sends structured HTML snippets to a local LLM (Ollama/Qwen) to extract flight numbers, carbon emissions, and luggage policies.
- **Validation**: Results are cross-referenced and deduplicated to ensure 100% accuracy.

---

## 🔧 Core Features

### 📅 Advanced Search
- Supports multi-airport city codes (e.g., LON, PAR, NYC).
- Fully configurable Cabin Classes (Economy, Business, First).
- Direct "Results-Only" navigation for maximum speed.

### 🛡️ Resilience & Stability
- **Overlay Crusher**: Automatically scrolls and expands "Other flights" to ensure all options are visible.
- **Unique Artifacts**: Generates timestamped screenshots (`flight_results_<timestamp>.png`) to prevent concurrent search processes from overwriting diagnostic data.
- **Patience Strategies**: Implements progressive waiting (up to 30s) to accommodate slow renders on Raspberry Pi hardware.

### 📊 Monitor Integration
- Real-time diagnostic logging available in the Monitor UI.
- Direct links to captured screenshots for human verification through the **Work Visualizer**.

---

## ⚙️ Configuration (K3s / Local)

Key environment variables:
- `SCRAPER_URL`: The URL of the Playwright Scraper (e.g., `http://playwright-scraper:8085`).
- `FLIGHT_SCRAPER_SCRIPT`: (Optional) A custom Javascript snippet to override the default navigation logic.
- `LLM_PROVIDER`: Set to `ollama` or `openai` for the SMART Miner.

---

## 🚀 Execution Flow

1. **Request**: User asks "Find me flights from London to Paris for next Monday."
2. **Translation**: HDN planning engine calls `search_flights` with structured dates and codes.
3. **Offloading**: Flight MCP sends a TypeScript navigation config to the Scraper Service.
4. **Scrape**: Scraper launches Chromium, handles Google Consent, performs the search, scrolls to load all flights, and takes a screenshot.
5. **Extract**: Flight MCP receives the screenshot and HTML. It runs OCR and LLM Mining in parallel.
6. **Result**: A structured list of flights is returned to the user, complete with prices and airline details.
