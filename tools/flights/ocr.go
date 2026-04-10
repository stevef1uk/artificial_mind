package main

import (
	"log"
	"regexp"
	"strings"

	"github.com/otiai10/gosseract/v2"
)

func ExtractFlightsFromImage(imagePath string) ([]FlightInfo, error) {
	client := gosseract.NewClient()
	defer client.Close()
	if err := client.SetImage(imagePath); err != nil { return nil, err }
	text, err := client.Text()
	if err != nil { return nil, err }
    
	log.Printf("📸 OCR extracted %d chars. Parsing...", len(text))
    
    flights := ParseFlightText(text)
    
    if len(flights) == 0 && len(text) > 100 {
        log.Println("⚠️ Regular OCR parse failed. Using Miner on OCR text...")
        return MinerExtractFromText(text)
    }
    
	return flights, nil
}

func ParseFlightText(text string) []FlightInfo {
	lines := strings.Split(text, "\n")
	var flights []FlightInfo

	// Comprehensive time regex: support 12h (10:30 AM) and 24h (10:30) formats
	// Also handle different dashes used by Google (en-dash, em-dash, hyphen)
	timeRegex := regexp.MustCompile(`(?i)(\d{1,2}:\d{2}(?:\s*[AP]M)?)\s*[–—~-]\s*(\d{1,2}:\d{2}(?:\s*[AP]M)?)`)
	priceRegex := regexp.MustCompile(`([€£\$])\s*([\d,\.]+)`)
	durationRegex := regexp.MustCompile(`(\d+)\s*hr\s*(\d+)?\s*min`)
	stopRegex := regexp.MustCompile(`(\d+)\s*stop|Nonstop`)
	routeRegex := regexp.MustCompile(`\b([A-Z]{3})\s*[^A-Z0-9]?\s*([A-Z]{3})\b`)

	airlines := []string{
		"Virgin Atlantic", "British Airways", "Air France", "Delta", "KLM", "United", 
		"American", "Icelandair", "Lufthansa", "Turkish Airlines", "Emirates", "Qatar",
		"Iberia", "Swiss", "Aer Lingus", "TAP", "Austrian", "SAS", "Finnair", "LOT", 
		"Brussels Airlines", "ITA Airways", "JetBlue", "Norse", "Vueling", "Ryanair", "EasyJet", "easyJet",
		"AirFrance", "BritishAirways", "Transavia", "Wizz Air", "Eurowings", "Norwegian",
	}

	for i, line := range lines {
		timeMatch := timeRegex.FindStringSubmatch(line)
		if len(timeMatch) > 0 {
			flight := FlightInfo{
				DepartureTime: timeMatch[1], ArrivalTime: timeMatch[2],
				Airline: "Unknown", Price: "Unknown", Duration: "Unknown", Stops: "Unknown",
				DepartureAirport: "Unknown", ArrivalAirport: "Unknown",
			}

			// Wider search window (15 lines) to catch labels that might be separated by ads or styling
			start, end := i-5, i+12
			if start < 0 { start = 0 }
			if end > len(lines) { end = len(lines) }

			for j := start; j < end; j++ {
				l := lines[j]
				if pm := priceRegex.FindStringSubmatch(l); len(pm) > 0 && flight.Price == "Unknown" {
					symbol := pm[1]
					// Remove commas (thousands separators) but keep dots (decimal separators)
					val := strings.ReplaceAll(pm[2], ",", "")
					// CRITICAL: Prevent OCR hallucinations of massive prices
					if len(val) > 4 { 
						log.Printf("⚠️ Filtering suspicious price: %s", val)
						continue 
					}
					flight.Price = symbol + val
				}
				if dm := durationRegex.FindStringSubmatch(l); len(dm) > 0 && flight.Duration == "Unknown" {
					flight.Duration = dm[0]
				}
				if sm := stopRegex.FindStringSubmatch(l); len(sm) > 0 && flight.Stops == "Unknown" {
					flight.Stops = sm[0]
				}
				if rm := routeRegex.FindStringSubmatch(l); len(rm) > 0 && flight.DepartureAirport == "Unknown" {
					flight.DepartureAirport = rm[1]
					flight.ArrivalAirport = rm[2]
				}
				if flight.Airline == "Unknown" {
					for _, a := range airlines {
						if strings.Contains(strings.ToLower(l), strings.ToLower(a)) {
							flight.Airline = a; break
						}
					}
				}
			}

			if flight.Price != "Unknown" {
				flights = append(flights, flight)
				log.Printf("✅ Found Flight: %s %s at %s (%s to %s)", flight.Airline, flight.Price, flight.DepartureTime, flight.DepartureAirport, flight.ArrivalAirport)
			}
		}
	}
	return flights
}

func MinerExtractFromText(text string) ([]FlightInfo, error) {
    return MinerExtractFlights(text) 
}
