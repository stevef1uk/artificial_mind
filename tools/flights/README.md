# Flights Search in Go

This is a Go rewrite of the flight search tool that uses Playwright for browser automation and OCR (Tesseract) to extract flight details from Google Flights.

## Prerequisites

1.  **Go**: Install from [golang.org](https://golang.org/dl/)
2.  **Tesseract OCR**: Install on your system:
    -   macOS: `brew install tesseract`
    -   Ubuntu: `sudo apt-get install tesseract-ocr libtesseract-dev`
3.  **Playwright for Go**:
    -   Install the driver: `go run github.com/playwright-community/playwright-go/cmd/playwright@latest install --with-deps`

## Setup

1.  Initialize the Go module:
    ```bash
    go mod init flights_go
    ```
2.  Install dependencies:
    ```bash
    go get github.com/playwright-community/playwright-go
    go get github.com/otiai10/gosseract/v2
    ```

## Usage

Run the program with the following flags:

```bash
go run . -departure JFK -destination CDG -start_date 2024-05-01 -end_date 2024-05-10 -cabin Business
```

### Parameters:

-   `-departure`: Departure airport code (e.g., JFK, LAX)
-   `-destination`: Destination airport code (e.g., CDG, LHR)
-   `-start_date`: Departure date in YYYY-MM-DD format
-   `-end_date`: Return date in YYYY-MM-DD format
-   `-cabin`: Cabin class (Economy, Business, First, Premium Economy). Default: Economy

## How it works

1.  **Playwright** launches a Chromium browser and navigates to the constructed Google Flights URL.
2.  It takes a **screenshot** of the results page.
3.  **Tesseract OCR** extracts text from the screenshot.
4.  The program **parses** the extracted text using regex to associate times, prices, airlines, and durations based on line proximity.
5.  It identifies and displays the **cheapest flight** option.
