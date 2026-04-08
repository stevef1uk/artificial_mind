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

	timeRegex := regexp.MustCompile(`(?i)(\d{1,2}:\d{2}\s*[AP]M)\s*[–—~-]\s*(\d{1,2}:\d{2}\s*[AP]M)`)
	priceRegex := regexp.MustCompile(`[€£]\s*([\d,\.]+)`)
	durationRegex := regexp.MustCompile(`(\d+)\s*hr\s*(\d+)?\s*min`)
	stopRegex := regexp.MustCompile(`(\d+)\s*stop|Nonstop`)

	airlines := []string{
		"Virgin Atlantic", "British Airways", "Air France", "Delta", "KLM", "United", 
		"American", "Icelandair", "Lufthansa", "Turkish Airlines", "Emirates", "Qatar",
		"Iberia", "Swiss", "Aer Lingus", "TAP", "Austrian", "SAS", "Finnair", "LOT", 
		"Brussels Airlines", "ITA Airways", "JetBlue", "Norse", "Vueling", "Ryanair", "EasyJet", "easyJet",
        "AirFrance", "BritishAirways", // Handle potential OCR concatenation
	}

	for i, line := range lines {
		timeMatch := timeRegex.FindStringSubmatch(line)
		if len(timeMatch) > 0 {
			flight := FlightInfo{
				DepartureTime: timeMatch[1], ArrivalTime: timeMatch[2],
				Airline: "Unknown", Price: "Unknown", Duration: "Unknown", Stops: "Unknown",
			}

			start, end := i-4, i+10
			if start < 0 { start = 0 }
			if end > len(lines) { end = len(lines) }

			for j := start; j < end; j++ {
				l := lines[j]
				if pm := priceRegex.FindStringSubmatch(l); len(pm) > 0 && flight.Price == "Unknown" {
                    symbol := "£"
                    if strings.Contains(l, "€") { symbol = "€" }
					flight.Price = symbol + strings.ReplaceAll(strings.ReplaceAll(pm[1], ",", ""), ".", "")
				}
				if dm := durationRegex.FindStringSubmatch(l); len(dm) > 0 && flight.Duration == "Unknown" {
					flight.Duration = dm[0]
				}
				if sm := stopRegex.FindStringSubmatch(l); len(sm) > 0 && flight.Stops == "Unknown" {
					flight.Stops = sm[0]
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
				log.Printf("✅ Found Flight: %s %s at %s", flight.Airline, flight.Price, flight.DepartureTime)
			}
		}
	}
	return flights
}

func MinerExtractFromText(text string) ([]FlightInfo, error) {
    return MinerExtractFlights(text) 
}
