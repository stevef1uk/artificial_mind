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
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var globalOptions *SearchOptions

func main() {
	// log to Stderr to avoid polluting Stdout which might be used for MCP transport
	log.Println("🚀 FLIGHT MCP VERSION 58 STARTING...")
    
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
		mcp.WithDescription("Search for flights on Google Flights. Provide a natural language query or structured parameters."),
		mcp.WithString("query", mcp.Description("Natural language search string (e.g., 'morning business flights from Lisbon to Rio tomorrow')")),
		mcp.WithString("departure", mcp.Description("Departure airport code")),
		mcp.WithString("destination", mcp.Description("Destination airport code")),
		mcp.WithString("start_date", mcp.Description("Departure date (YYYY-MM-DD)")),
		mcp.WithString("end_date", mcp.Description("Return date (YYYY-MM-DD)")),
		mcp.WithString("cabin", mcp.Description("Travel class (Economy, Business, First)")),
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
	if query := getStr("query"); query != "" {
		log.Printf("🧠 Extracting parameters from query: %s", query)
		extracted, err := ExtractOptionsFromQuery(query)
		if err == nil {
			// If extracted fields are non-empty, prioritize them over potentially hallucinated args
			if extracted.Departure != "" { opts.Departure = extracted.Departure }
			if extracted.Destination != "" { opts.Destination = extracted.Destination }
			if extracted.StartDate != "" { opts.StartDate = extracted.StartDate }
			if extracted.EndDate != "" { opts.EndDate = extracted.EndDate }
			if extracted.CabinClass != "" { opts.CabinClass = extracted.CabinClass }
            
			// Manual high-confidence override for cabin
			lowQuery := strings.ToLower(query)
			if strings.Contains(lowQuery, "business") {
				opts.CabinClass = "Business"
			} else if strings.Contains(lowQuery, "first") {
				opts.CabinClass = "First"
			}
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
		"LONDON": "LON", "PARIS": "PAR", "NEW YORK": "NYC", "NYC": "NYC",
		"LISBON": "LIS", "LISBOA": "LIS", "RIO": "RIO", "RIO DE JANEIRO": "RIO",
		"RIO DE JENERIO": "RIO", "SAO PAULO": "SAO", "TOKYO": "TYO",
		"LHR": "LON", "LGW": "LON", "LTN": "LON", "STN": "LON", "LCY": "LON",
		"CDG": "PAR", "ORY": "PAR", "BVA": "PAR",
		"EWR": "NYC", "JFK": "NYC", "LGA": "NYC",
		"GIG": "RIO", "SDU": "RIO",
	}

	// 1. Resolve full names to codes using mapping
	if code, ok := cityMappings[strings.ToUpper(opts.Departure)]; ok {
		opts.Departure = code
	}
	if code, ok := cityMappings[strings.ToUpper(opts.Destination)]; ok {
		opts.Destination = code
	}

	// 2. If still not a 3-letter code, use LLM to resolve (e.g. "San Francisco" -> "SFO")
	iataRegex := regexp.MustCompile(`^[A-Z]{3}$`)
	if !iataRegex.MatchString(strings.ToUpper(opts.Departure)) || !iataRegex.MatchString(strings.ToUpper(opts.Destination)) {
		log.Printf("🏙️ Resolving vague city names via NLP: %s -> %s", opts.Departure, opts.Destination)
		q := fmt.Sprintf("flight from %s to %s", opts.Departure, opts.Destination)
		extracted, err := ExtractOptionsFromQuery(q)
		if err == nil {
			if iataRegex.MatchString(extracted.Departure) { opts.Departure = extracted.Departure }
			if iataRegex.MatchString(extracted.Destination) { opts.Destination = extracted.Destination }
		}
	}

	// 3. Past Date Prevention: If year is in the past, bump to current year
	currentYear := time.Now().Year()
	dateFix := func(d string) string {
		if len(d) >= 4 {
			y, _ := strconv.Atoi(d[:4])
			if y < currentYear {
				log.Printf("📅 Correcting past year: %s -> %d%s", d, currentYear, d[4:])
				return fmt.Sprintf("%d%s", currentYear, d[4:])
			}
		}
		return d
	}
	opts.StartDate = dateFix(opts.StartDate)
	opts.EndDate = dateFix(opts.EndDate)
	
	// 4. Final broadening (e.g. LHR -> LON)
	// ONLY broaden if we don't have a specific 3-letter code, or if the user's intent was general.
	// But actually, Google Flights is better if we search LON instead of LHR for variety.
    // HOWEVER, if the user is complaining about 'regression', maybe they want their specific airport.
	origDep, origDest := opts.Departure, opts.Destination
    
    // Heuristic: If it's already a 3-letter IATA, don't broaden UNLESS it's a known city group and we want to be helpful. 
    // Let's refine this: broaden but KEEP the original for the validation filter.
	if city, ok := cityMappings[strings.ToUpper(opts.Departure)]; ok {
        // Only broaden if it WASN'T a 3-letter code (i.e., it was 'London')
        if len(origDep) != 3 {
		    opts.Departure = city
        }
	}
	if city, ok := cityMappings[strings.ToUpper(opts.Destination)]; ok {
        if len(origDest) != 3 {
		    opts.Destination = city
        }
	}
	
	if opts.Departure != origDep || opts.Destination != origDest {
		log.Printf("🏙️  Broadening search: %s -> %s to %s -> %s", origDep, origDest, opts.Departure, opts.Destination)
	}

    // FINAL CABIN HARDENING: Priority detection for Business/First class
    if lowQuery := strings.ToLower(getStr("query")); strings.Contains(lowQuery, "business") {
        opts.CabinClass = "Business"
    } else if strings.Contains(lowQuery, "first") {
        opts.CabinClass = "First"
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
		"LON": {"LHR", "LGW", "LTN", "STN", "LCY", "SEN", "LUT"},
		"PAR": {"CDG", "ORY", "BVA"},
		"NYC": {"JFK", "EWR", "LGA"},
		"RIO": {"GIG", "SDU"},
		"SAO": {"GRU", "CGH", "VCP"},
		"STO": {"ARN", "BMA", "NYO", "VST"},
		"MIL": {"MXP", "LIN", "BGY"},
		"CHI": {"ORD", "MDW"},
		"WAS": {"IAD", "DCA", "BWI"},
		"TYO": {"NRT", "HND"},
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
		// For round trips, we accept flights in both directions
		matchesNormal := (dep == "UNKNOWN" || dep == "" || isMember(dep, validDeparture)) && 
						 (arr == "UNKNOWN" || arr == "" || isMember(arr, validDestination))
		
		matchesReverse := (dep == "UNKNOWN" || dep == "" || isMember(dep, validDestination)) && 
						  (arr == "UNKNOWN" || arr == "" || isMember(arr, validDeparture))
		
		if matchesNormal || matchesReverse {
			filtered = append(filtered, f)
		} else {
			log.Printf("🚫 Filtered out mismatched route: %s -> %s (Target: %s -> %s)", dep, arr, validDeparture, validDestination)
		}
	}
	flights = filtered

	if len(flights) == 0 {
		return mcp.NewToolResultText("No flights found matching the requested route."), nil
	}

	// Read and encode screenshot if available
	var imageContent mcp.ImageContent
	hasImage := false
	if screenshotPath != "" {
		imgData, err := os.ReadFile(screenshotPath)
		if err == nil {
			imageContent = mcp.NewImageContent(base64.StdEncoding.EncodeToString(imgData), "image/png")
			hasImage = true
		}
	}

	// Generate structured JSON for the reasoning engine
	jsonData, _ := json.MarshalIndent(flights, "", "  ")

	// Generate a clean summary for the chat response
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("SUCCESS: Found %d flight options for your trip from %s to %s (Dates: %s to %s).\n\n", len(flights), opts.Departure, opts.Destination, opts.StartDate, opts.EndDate))
	sb.WriteString("Flight Details (Departing):\n")
	for i, f := range flights {
		sb.WriteString(fmt.Sprintf("• %s: %s (Time: %s - %s) Route: %s -> %s\n", f.Airline, f.Price, f.DepartureTime, f.ArrivalTime, f.DepartureAirport, f.ArrivalAirport))
		if i == 7 { // Show top 8 in text summary for better coverage
			sb.WriteString("\n... [Full list of all %d flights available in DATA_JSON block below]")
			break
		}
	}

	results := []mcp.Content{
		mcp.NewTextContent(sb.String()),
		mcp.NewTextContent(fmt.Sprintf("DATA_JSON: %s", string(jsonData))),
	}
	if hasImage {
		results = append(results, imageContent)
	}

	return &mcp.CallToolResult{
		Content: results,
	}, nil
}
