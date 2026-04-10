# ✈️ Flight Search Tool

This tool is part of the **Artificial Mind Flight Service**. 

It uses a distributed architecture to offload browser automation to a dedicated scraper service while performing high-fidelity extraction using a hybrid OCR and LLM-based mining strategy.

## 🏛️ Architecture & Documentation

For a detailed technical overview of how this service works, including K3s configuration and extraction tracks, please see the official documentation:

👉 [**Flight Search Service Architecture**](../../docs/FLIGHT_SERVICE.md)

## High-Level Features
- **MCP Integration**: Exposes `search_flights` to the reasoning engine.
- **Offloaded Browser**: Offloads Playwright tasks to the `playwright_scraper` service.
- **Hybrid Extraction**: Combines Tesseract OCR with a SMART LLM Miner.
- **K3s Optimized**: Designed for reliability on Raspberry Pi clusters.
