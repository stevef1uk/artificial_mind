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
	// Added flexibility for spacing around separators (e.g. "10 . 25") common in some OCR versions.
	timeRegex := regexp.MustCompile(`(?i)(\d{1,2}[\s]*[:\.]?[\s]*\d{1,2}(?:[\s]*[AP]M)?)\s*[–—~-]\s*(\d{1,2}[\s]*[:\.]?[\s]*\d{1,2}(?:[\s]*[AP]M)?)`)
	priceRegex := regexp.MustCompile(`(?:^|[\s])([€£\$])\s*(\d{1,4}(?:[\.,]\d{2})?)\b`)
	durationRegex := regexp.MustCompile(`(\d+)\s*hr\s*(\d+)?\s*min`)
	stopRegex := regexp.MustCompile(`(?i)(\d+)\s*stop|Nonstop`)
	// Be more specific: require a dash, slash, or dot between codes to avoid 'MIN NON'
	routeRegex := regexp.MustCompile(`(?i)\b([A-Z]{3})[\s]*[-—–/][\s]*([A-Z]{3})\b`)

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

			// Look ahead up to 12 lines to find flight details.
			// Starting at i ensures we don't pick up data from the previous flight row.
			start, end := i, i+12
			if start < 0 { start = 0 }
			if end > len(lines) { end = len(lines) }

			// Priority 1: Check same line for price (most accurate)
			if pm := priceRegex.FindStringSubmatch(line); len(pm) > 0 {
				symbol, val := pm[1], strings.ReplaceAll(pm[2], ",", "")
				if p := parsePrice(val); p > 0 && p < 2500 {
					flight.Price = symbol + val
				}
			}

			// Priority 2: Check surrounding lines
			for j := start; j < end; j++ {
				l := lines[j]
				if pm := priceRegex.FindStringSubmatch(l); len(pm) > 0 && flight.Price == "Unknown" {
					symbol, val := pm[1], strings.ReplaceAll(pm[2], ",", "")
					if p := parsePrice(val); p > 0 && p < 2500 {
						flight.Price = symbol + val
					}
				}
				if dm := durationRegex.FindStringSubmatch(l); len(dm) > 0 && flight.Duration == "Unknown" {
					flight.Duration = dm[0]
				}
				if sm := stopRegex.FindStringSubmatch(l); len(sm) > 0 && flight.Stops == "Unknown" {
					flight.Stops = sm[0]
				}
				if rm := routeRegex.FindStringSubmatch(l); len(rm) > 0 && flight.DepartureAirport == "Unknown" {
					flight.DepartureAirport = strings.ToUpper(rm[1])
					flight.ArrivalAirport = strings.ToUpper(rm[2])
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
