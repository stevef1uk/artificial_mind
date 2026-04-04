package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	transportType := flag.String("transport", "sse", "Transport type (stdio, sse, or http)")
	port := flag.Int("port", 8080, "Port for network transport")
	flag.Parse()

	// Create MCP server
	s := server.NewMCPServer(
		"Flights Search",
		"1.0.0",
		server.WithLogging(),
	)

	// Add search_flights tool
	s.AddTool(mcp.NewTool("search_flights",
		mcp.WithDescription("Search for flights on Google Flights using Playwright and OCR [chat-only]"),
		mcp.WithString("departure", mcp.Required(), mcp.Description("Departure airport code (e.g., JFK, LAX)")),
		mcp.WithString("destination", mcp.Required(), mcp.Description("Destination airport code (e.g., CDG, LHR)")),
		mcp.WithString("start_date", mcp.Required(), mcp.Description("Departure date (YYYY-MM-DD)")),
		mcp.WithString("end_date", mcp.Required(), mcp.Description("Return date (YYYY-MM-DD)")),
		mcp.WithString("cabin", mcp.Description("Cabin class (Economy, Business, First, Premium Economy). Default: Economy")),
	), searchFlightsHandler)

	if *transportType == "sse" {
		sseServer := server.NewSSEServer(s, server.WithBaseURL(fmt.Sprintf("http://localhost:%d", *port)))
		log.Printf("Starting SSE server on :%d", *port)
		if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), sseServer); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	} else if *transportType == "http" {
		// Simple HTTP handler for basic clients like HDN's current MCPClient
		log.Printf("Starting Simple HTTP server on :%d", *port)
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var rawMessage json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&rawMessage); err != nil {
				http.Error(w, "Parse error", http.StatusBadRequest)
				return
			}
			// Use the internal MCPServer to handle the message directly
			resp := s.HandleMessage(r.Context(), rawMessage)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		})
		if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	} else {
		if err := server.ServeStdio(s); err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	}
}

func searchFlightsHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]interface{})
	if !ok {
		return mcp.NewToolResultError("failed to parse arguments"), nil
	}

	departure, _ := args["departure"].(string)
	destination, _ := args["destination"].(string)
	startDate, _ := args["start_date"].(string)
	endDate, _ := args["end_date"].(string)
	cabin, _ := args["cabin"].(string)

	if cabin == "" {
		cabin = "Economy"
	}

	log.Printf("Searching for %s flights from %s to %s from %s to %s...", cabin, departure, destination, startDate, endDate)

	flights, err := SearchFlights(departure, destination, startDate, endDate, cabin)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Error searching flights: %v", err)), nil
	}

	if len(flights) == 0 {
		return mcp.NewToolResultText("No flights found."), nil
	}

	var cheapest *FlightInfo
	minPrice := 999999
	
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d flight options:\n\n", len(flights)))

	for i, f := range flights {
		sb.WriteString(fmt.Sprintf("[%d] %s: %s (%s, %s)\n", i+1, f.Airline, f.Price, f.Duration, f.Stops))
		sb.WriteString(fmt.Sprintf("    Times: %s - %s\n", f.DepartureTime, f.ArrivalTime))
		sb.WriteString(fmt.Sprintf("    URL: %s\n\n", f.URL))
		
		priceVal := parsePrice(f.Price)
		if priceVal > 0 && priceVal < minPrice {
			minPrice = priceVal
			cheapest = &flights[i]
		}
	}

	if cheapest != nil {
		sb.WriteString("--- Cheapest Option ---\n")
		sb.WriteString(fmt.Sprintf("Airline: %s\n", cheapest.Airline))
		sb.WriteString(fmt.Sprintf("Price: %s\n", cheapest.Price))
		sb.WriteString(fmt.Sprintf("Duration: %s\n", cheapest.Duration))
		sb.WriteString(fmt.Sprintf("Times: %s - %s\n", cheapest.DepartureTime, cheapest.ArrivalTime))
		sb.WriteString(fmt.Sprintf("URL: %s\n", cheapest.URL))
	}

	return mcp.NewToolResultText(sb.String()), nil
}
