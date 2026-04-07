package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
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
		mcp.WithDescription("Search for flights on Google Flights using NATIVE Playwright and AI extraction"),
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
	opts := SearchOptions{
		Departure:   args["departure"].(string),
		Destination: args["destination"].(string),
		StartDate:   args["start_date"].(string),
		EndDate:     args["end_date"].(string),
		CabinClass:  "Economy",
		Language:    globalOptions.Language,
		Region:      globalOptions.Region,
		Currency:    globalOptions.Currency,
		Headless:    globalOptions.Headless,
		BrowserPath: globalOptions.BrowserPath,
	}
	if c, ok := args["cabin"].(string); ok && c != "" { opts.CabinClass = c }

	log.Printf("🔍 Searching for %s flights: %s -> %s (%s to %s)", opts.CabinClass, opts.Departure, opts.Destination, opts.StartDate, opts.EndDate)

	flights, err := SearchFlights(opts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
	}

	if len(flights) == 0 {
		return mcp.NewToolResultText("No flights found."), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d flight options:\n\n", len(flights)))
	for i, f := range flights {
		sb.WriteString(fmt.Sprintf("[%d] %s: %s (%s, %s)\n", i+1, f.Airline, f.Price, f.Duration, f.Stops))
		sb.WriteString(fmt.Sprintf("    Times: %s - %s\n", f.DepartureTime, f.ArrivalTime))
		sb.WriteString(fmt.Sprintf("    URL: %s\n\n", f.URL))
	}

	return mcp.NewToolResultText(sb.String()), nil
}
