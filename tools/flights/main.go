package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var globalOptions *SearchOptions

func main() {
	fmt.Println("🚀 FLIGHT MCP VERSION 56 STARTING...")
    
	transportType := flag.String("transport", "sse", "Transport type (stdio, sse, or http)")
	port := flag.Int("port", 8080, "Port for network transport")
	lang := flag.String("lang", "en", "Google Flights language code (hl)")
	region := flag.String("region", "US", "Google Flights region code (gl)")
	currency := flag.String("currency", "EUR", "Google Flights currency code (curr)")
	headless := flag.Bool("headless", true, "Run browser in headless mode")
	browser := flag.String("browser", "/usr/bin/chromium", "Path to chromium executable")
	flag.Parse()

	globalOptions = &SearchOptions{
		Language:    *lang,
		Region:      *region,
		Currency:    *currency,
		Headless:    *headless,
		BrowserPath: *browser,
	}

	s := server.NewMCPServer("Flights Search", "1.1.0", server.WithLogging())

	s.AddTool(mcp.NewTool("search_flights",
		mcp.WithDescription("Search for flights on Google Flights using Playwright and AI-powered data extraction"),
		mcp.WithString("departure", mcp.Required(), mcp.Description("Departure airport code (e.g., JFK, LAX)")),
		mcp.WithString("destination", mcp.Required(), mcp.Description("Destination airport code (e.g., CDG, LHR)")),
		mcp.WithString("start_date", mcp.Required(), mcp.Description("Departure date (YYYY-MM-DD)")),
		mcp.WithString("end_date", mcp.Required(), mcp.Description("Return date (YYYY-MM-DD)")),
		mcp.WithString("cabin", mcp.Description("Cabin class (Economy, Business, First, Premium Economy). Default: Economy")),
	), searchFlightsHandler)

	if *transportType == "sse" {
		sseServer := server.NewSSEServer(s, server.WithBaseURL(fmt.Sprintf("http://localhost:%d", *port)))
		log.Printf("Starting SSE server on :%d", *port)
		http.ListenAndServe(fmt.Sprintf(":%d", *port), sseServer)
	} else if *transportType == "http" {
		log.Printf("Starting Simple HTTP server on :%d", *port)
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			var rawMessage json.RawMessage
			json.NewDecoder(r.Body).Decode(&rawMessage)
			resp := s.HandleMessage(r.Context(), rawMessage)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		})
		http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
	} else {
		server.ServeStdio(s)
	}
}

func searchFlightsHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := request.Params.Arguments.(map[string]interface{})
	log.Printf("🛠️ Received tool arguments: %+v", args)
	
	// Safe argument extraction
	getStr := func(key string) string {
		if v, ok := args[key].(string); ok {
			return v
		}
		return ""
	}

	opts := SearchOptions{
		Departure:   getStr("departure"),
		Destination: getStr("destination"),
		StartDate:   getStr("start_date"),
		EndDate:     getStr("end_date"),
		CabinClass:  "Economy",
		Language:    globalOptions.Language,
		Region:      globalOptions.Region,
		Currency:    globalOptions.Currency,
		Headless:    globalOptions.Headless,
		BrowserPath: globalOptions.BrowserPath,
	}
	if c := getStr("cabin"); c != "" {
		opts.CabinClass = c
	}

	// Handle natural language 'query' if provided (HDN fallback)
	if query := getStr("query"); query != "" && opts.Departure == "" {
		log.Printf("🧠 Extracting parameters from query: %s", query)
		extracted, err := ExtractOptionsFromQuery(query)
		if err == nil {
			opts.Departure = extracted.Departure
			opts.Destination = extracted.Destination
			opts.StartDate = extracted.StartDate
			opts.EndDate = extracted.EndDate
			if extracted.CabinClass != "" { opts.CabinClass = extracted.CabinClass }
		} else {
			log.Printf("⚠️ Query extraction failed: %v", err)
		}
	}

	// Normalizer: ensure dates are in YYYY-MM-DD format before search
	dateRegex := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	if (opts.StartDate != "" && !dateRegex.MatchString(opts.StartDate)) || (opts.EndDate != "" && !dateRegex.MatchString(opts.EndDate)) {
		log.Printf("📅 Normalizing dates from natural language: %s, %s", opts.StartDate, opts.EndDate)
		q := fmt.Sprintf("flights from %s to %s starting %s returning %s", opts.Departure, opts.Destination, opts.StartDate, opts.EndDate)
		extracted, err := ExtractOptionsFromQuery(q)
		if err == nil {
			if extracted.StartDate != "" { opts.StartDate = extracted.StartDate }
			if extracted.EndDate != "" { opts.EndDate = extracted.EndDate }
		}
	}

	// HEURISTIC: Broaden search for multi-airport cities if specific major ones are used
	// This helps find easyJet/Ryanair results from alternative airports (LTN, LGW, etc)
	cityMappings := map[string]string{
		"LHR": "LON", "LGW": "LON", "LTN": "LON", "STN": "LON", "LCY": "LON",
		"CDG": "PAR", "ORY": "PAR", "BVA": "PAR",
		"EWR": "NYC", "JFK": "NYC", "LGA": "NYC",
	}
	
	origDep, origDest := opts.Departure, opts.Destination
	if city, ok := cityMappings[strings.ToUpper(opts.Departure)]; ok {
		opts.Departure = city
	}
	if city, ok := cityMappings[strings.ToUpper(opts.Destination)]; ok {
		opts.Destination = city
	}
	
	if opts.Departure != origDep || opts.Destination != origDest {
		log.Printf("🏙️  Broadening search: %s -> %s to %s -> %s", origDep, origDest, opts.Departure, opts.Destination)
	}

	log.Printf("🔍 Searching for %s flights: %s -> %s (%s to %s)", opts.CabinClass, opts.Departure, opts.Destination, opts.StartDate, opts.EndDate)

	flights, screenshotPath, err := SearchFlights(opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
	}

	// VALIDATION FILTER: Remove hallucinated results (e.g. search for LON->PAR returns Delhi)
	validDeparture := strings.ToUpper(opts.Departure)
	validDestination := strings.ToUpper(opts.Destination)
	
	// Create a membership map for the city groups
	cityMembers := map[string][]string{
		"LON": {"LHR", "LGW", "LTN", "STN", "LCY", "SEN"},
		"PAR": {"CDG", "ORY", "BVA"},
		"NYC": {"JFK", "EWR", "LGA"},
	}
	
	isMember := func(code, group string) bool {
		if code == group { return true }
		if members, ok := cityMembers[group]; ok {
			for _, m := range members {
				if code == m { return true }
			}
		}
		return false
	}

	var filtered []FlightInfo
	for _, f := range flights {
		dep := strings.ToUpper(f.DepartureAirport)
		arr := strings.ToUpper(f.ArrivalAirport)
		
		// If both are unknown or at least one matches the city group, keep it
		// We are lenient if both are unknown to avoid dropping everything, 
		// but if we HAVE codes, we enforce them.
		if dep == "UNKNOWN" || dep == "" || isMember(dep, validDeparture) {
			if arr == "UNKNOWN" || arr == "" || isMember(arr, validDestination) {
				filtered = append(filtered, f)
			}
		}
	}
	flights = filtered

	if len(flights) == 0 {
		return mcp.NewToolResultText("No flights found matching the requested route."), nil
	}

	// Read and encode screenshot if available
	var imageContent *mcp.ImageContent
	if screenshotPath != "" {
		imgData, err := os.ReadFile(screenshotPath)
		if err == nil {
			imageContent = mcp.NewImageContent(base64.StdEncoding.EncodeToString(imgData), "image/png")
		}
	}

	// Generate structured JSON for the reasoning engine
	jsonData, _ := json.MarshalIndent(flights, "", "  ")

	// Generate a summary for the chat response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d flight options from %s to %s on %s. (Luton/Gatwick/Heathrow are included in LON group)\n", len(flights), opts.Departure, opts.Destination, opts.StartDate))
	for i, f := range flights {
		sb.WriteString(fmt.Sprintf("[%d] %s: %s (%s to %s, %s, %s)\n", i+1, f.Airline, f.Price, f.DepartureAirport, f.ArrivalAirport, f.Duration, f.Stops))
		if i == 4 { // Only show top 5 in text summary to keep it clean
			sb.WriteString("... [truncated, see JSON for full list]\n")
			break
		}
	}

	results := []mcp.Content{
		mcp.NewTextContent(sb.String()),
		mcp.NewTextContent(fmt.Sprintf("DATA_JSON: %s", string(jsonData))),
	}
	if imageContent != nil {
		results = append(results, *imageContent)
	}

	return &mcp.CallToolResult{
		Content: results,
	}, nil
}
