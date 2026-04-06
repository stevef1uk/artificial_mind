package main

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/otiai10/gosseract/v2"
)

// ExtractFlightsFromImage performs OCR on the image and parses the text for flight details
func ExtractFlightsFromImage(imagePath string) ([]FlightInfo, error) {
	client := gosseract.NewClient()
	defer client.Close()
	
	if err := client.SetImage(imagePath); err != nil {
		return nil, fmt.Errorf("could not set image: %v", err)
	}
	
	text, err := client.Text()
	if err != nil {
		return nil, fmt.Errorf("could not perform OCR: %v", err)
	}
	
	preview := text
	if len(preview) > 500 {
		preview = preview[:500]
	}
	log.Printf("Extracted text (length %d, preview): %s", len(text), preview)
	
	return ParseFlightText(text), nil
}

func ParseFlightText(text string) []FlightInfo {
	lines := strings.Split(text, "\n")
	var flights []FlightInfo

	// Regex to match flight times (e.g., 10:15 AM - 1:45 PM)
	timeRegex := regexp.MustCompile(`(\d{1,2}:\d{2}\s*[APap][Mm])\s*[-–~]\s*(\d{1,2}:\d{2}\s*[APap][Mm])`)
	// Regex to match price (e.g., € 1,234, £ 567, $ 890)
	priceRegex := regexp.MustCompile(`[€£$]\s*([\d,\.]+)`)
	// Regex to match duration (e.g., 8 hr 30 min)
	durationRegex := regexp.MustCompile(`(\d+)\s*hr\s*(\d+)?\s*min`)
	// Regex to match stops
	stopRegex := regexp.MustCompile(`(\d+)\s*stop|Nonstop`)

	// Exclusion patterns for dynamic airline detection
	airportRegex := regexp.MustCompile(`^[A-Z]{3}(-[A-Z]{3})?(\s+[A-Z]{3})?$`)
	numericRegex := regexp.MustCompile(`^[\d\s\.,]+(kg|adult|child|infant)?$`)
	cleanRegex := regexp.MustCompile(`[^a-zA-Z\s]`)
	
	forbiddenWords := []string{
		"Roundtrip", "Economy", "Stops", "Airlines", "Bags", "Price", "Times", 
		"Emissions", "Duration", "Departing", "Returning", "Filters", "Best", 
		"Cheapest", "Options", "Search", "Google", "Travel", "Explore", "Hotels",
		"Passenger", "Assistance", "Track", "History", "Date", "Grid", "Graph",
		"Train", "Trains", "Eurostar", "Railway", "Coach", "Bus", "Station",
	}

	for i, line := range lines {
		timeMatch := timeRegex.FindStringSubmatch(line)
		if len(timeMatch) > 0 {
			flight := FlightInfo{
				DepartureTime: timeMatch[1],
				ArrivalTime:   timeMatch[2],
				Airline:       "Unknown",
				Price:         "Unknown",
				Duration:      "Unknown",
				Stops:         "Unknown",
			}

			// Look for airline, price, duration, stops in nearby lines (within 8 lines before, 15 lines after)
			start := i - 8
			if start < 0 {
				start = 0
			}
			end := i + 15
			if end > len(lines) {
				end = len(lines)
			}

			isTrain := false
			for j := start; j < end; j++ {
				l := strings.TrimSpace(lines[j])
				if l == "" {
					continue
				}

				// DETECT TRAINS
				lowL := strings.ToLower(l)
				if strings.Contains(lowL, "train") || strings.Contains(lowL, "eurostar") || strings.Contains(lowL, "railway") {
					isTrain = true
					break
				}
				
				// Price
				if flight.Price == "Unknown" {
					pm := priceRegex.FindStringSubmatch(l)
					if len(pm) > 0 {
						// Clean currency string but keep symbol
						symbolMatch := regexp.MustCompile(`[€£$]`).FindString(l)
						if symbolMatch == "" {
							symbolMatch = "€" 
						}
						
						// Just take the digits and comma/dot
						pStr := pm[1]
						flight.Price = symbolMatch + pStr
						log.Printf("Detected price for %s: %s", flight.DepartureTime, flight.Price)
					}
				}

				// Duration
				if flight.Duration == "Unknown" {
					dm := durationRegex.FindStringSubmatch(l)
					if len(dm) > 0 {
						flight.Duration = dm[0]
					}
				}

				// Stops
				if flight.Stops == "Unknown" {
					sm := stopRegex.FindStringSubmatch(l)
					if len(sm) > 0 {
						flight.Stops = sm[0]
					}
				}

				// Dynamic Airline Detection
				// Scan a slightly wider range for the airline name
				if flight.Airline == "Unknown" && (j >= i-3 && j <= i+3) {
					cleanLine := cleanRegex.ReplaceAllString(l, "")
					cleanLine = strings.TrimSpace(cleanLine)
					
					isForbidden := false
					for _, fw := range forbiddenWords {
						if strings.Contains(strings.ToLower(cleanLine), strings.ToLower(fw)) {
							isForbidden = true
							break
						}
					}

					if !isForbidden && 
					   !timeRegex.MatchString(l) && 
					   !priceRegex.MatchString(l) && 
					   !durationRegex.MatchString(l) && 
					   !stopRegex.MatchString(l) && 
					   !airportRegex.MatchString(cleanLine) &&
					   !numericRegex.MatchString(strings.ToLower(cleanLine)) &&
					   len(cleanLine) > 2 {
						flight.Airline = cleanLine
						log.Printf("Detected airline for %s: %s", flight.DepartureTime, cleanLine)
					}
				}
			}

			// Only add if we found at least a price and it's NOT a train
			if !isTrain && flight.Price != "Unknown" {
				flights = append(flights, flight)
			}
		}
	}

	log.Printf("Found %d flight options in OCR text", len(flights))
	return flights
}

// Helper to convert price string to int for comparison
func parsePrice(priceStr string) int {
	re := regexp.MustCompile(`[\d]+`)
	matches := re.FindAllString(priceStr, -1)
	combined := strings.Join(matches, "")
	val, _ := strconv.Atoi(combined)
	return val
}
