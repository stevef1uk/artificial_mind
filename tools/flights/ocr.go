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
	
	log.Printf("Extracted text (first 500 chars): %s", text[:500])
	
	return ParseFlightText(text), nil
}

func ParseFlightText(text string) []FlightInfo {
	lines := strings.Split(text, "\n")
	var flights []FlightInfo

	// Regex to match flight times (e.g., 10:15 AM - 1:45 PM)
	timeRegex := regexp.MustCompile(`(\d{1,2}:\d{2}\s*[APap][Mm])\s*[-–~]\s*(\d{1,2}:\d{2}\s*[APap][Mm])`)
	// Regex to match price (e.g., € 1,234)
	priceRegex := regexp.MustCompile(`€\s*([\d,\.]+)`)
	// Regex to match duration (e.g., 8 hr 30 min)
	durationRegex := regexp.MustCompile(`(\d+)\s*hr\s*(\d+)?\s*min`)
	// Regex to match stops
	stopRegex := regexp.MustCompile(`(\d+)\s*stop|Nonstop`)

	airlines := []string{
		"Virgin Atlantic", "British Airways", "Air France",
		"Delta", "KLM", "United", "American", "Icelandair",
		"Lufthansa", "Turkish Airlines", "Emirates", "Qatar",
		"Iberia", "Swiss", "Aer Lingus", "TAP", "Austrian",
		"SAS", "Finnair", "LOT", "Brussels Airlines", "ITA Airways",
		"JetBlue", "Norse", "Vueling", "Ryanair", "EasyJet",
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

			// Look for airline, price, duration, stops in nearby lines (within 5 lines)
			start := i - 3
			if start < 0 {
				start = 0
			}
			end := i + 5
			if end > len(lines) {
				end = len(lines)
			}

			for j := start; j < end; j++ {
				l := lines[j]
				
				// Price
				if flight.Price == "Unknown" {
					pm := priceRegex.FindStringSubmatch(l)
					if len(pm) > 0 {
						pStr := strings.ReplaceAll(pm[1], ",", "")
						pStr = strings.ReplaceAll(pStr, ".", "")
						
						// If the price is very long (e.g. 6200), and it ends in 0, 
						// it might be picking up "0" from "round trip" or similar.
						// Usually prices are 3 or 4 digits.
						if len(pStr) > 3 && strings.HasSuffix(pStr, "0") {
							// heuristics: if it's > 2000 it might be a double zero issue
							val, _ := strconv.Atoi(pStr)
							if val > 2000 {
								pStr = pStr[:len(pStr)-1]
							}
						}

						flight.Price = "€" + pStr
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

				// Airline
				if flight.Airline == "Unknown" {
					for _, a := range airlines {
						if strings.Contains(l, a) {
							flight.Airline = a
							break
						}
					}
				}
			}

			// Only add if we found at least a price
			if flight.Price != "Unknown" {
				flights = append(flights, flight)
			}
		}
	}

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
