package main

import (
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/otiai10/gosseract/v2"
)

// ExtractFlightsFromImage parses OCR text for flight rows
func ExtractFlightsFromImage(imagePath string, maxPrice float64, opts SearchOptions) ([]FlightInfo, string, error) {
	client := gosseract.NewClient()
	defer client.Close()
	if err := client.SetImage(imagePath); err != nil { return nil, "", err }
	text, err := client.Text()
	if err != nil { return nil, "", err }
    
	log.Printf("📸 OCR extracted %d chars. Parsing (MaxPrice: %.0f)...", len(text), maxPrice)

    
    flights := ParseFlightText(text, maxPrice)
    
    if len(flights) == 0 && len(text) > 100 {
        log.Println("⚠️ Regular OCR parse failed. Using Miner on OCR text...")
        results, err := MinerExtractFromText(text, opts)
        return results, text, err
    }
    
	return flights, text, nil
}

// ParseFlightText performs the actual line-by-line regex work
func ParseFlightText(text string, maxPrice float64) []FlightInfo {
	lines := strings.Split(text, "\n")
	var flights []FlightInfo

	// Regexes optimized for Google Flights OCR
	// Support HH:MM, HH.MM, and HH MM formats
	timeRegex := regexp.MustCompile(`(\d{1,2}[:. ]\d{2})\s*(?:AM|PM|am|pm)?`)
	// Strict price regex: must start with symbol and have digits.
	// Allow 'E' and 'EUR' as common OCR / text variants for €
	priceRegex := regexp.MustCompile(`([€£$E]|EUR|GBP)\s*(\d{1,3}(?:[.,]\d{3})*(?:[.,]\d{2})?)`)
	durationRegex := regexp.MustCompile(`\d{1,2}h\s*\d{0,2}m?`)
	stopRegex := regexp.MustCompile(`(?i)(non-stop|\d+\s*stop)`)
	routeRegex := regexp.MustCompile(`\b([A-Z]{3})\b[[:space:]\-–—]+\b([A-Z]{3})\b`)

	airlines := []string{
		"Virgin Atlantic", "British Airways", "Air France", "Delta", "KLM", "United", 
		"American", "Icelandair", "Lufthansa", "Turkish Airlines", "Emirates", "Qatar",
		"Iberia", "Swiss", "Aer Lingus", "TAP", "Austrian", "SAS", "Finnair", "LOT", 
		"Azul", "Gol", "LATAM", "Latam Airlines", "Air Europa", "Royal Air Maroc", "Condor", "Iberia Express",
		"Brussels Airlines", "ITA Airways", "JetBlue", "Norse", "Vueling", "Ryanair", "EasyJet", "easyJet", "easydet",
		"AirFrance", "BritishAirways", "Transavia", "Wizz Air", "Eurowings", "Norwegian", "Air Baltic", "Swiss Railways",
	}

	for i, line := range lines {
		matches := timeRegex.FindAllStringSubmatch(line, -1)
		// We expect at least one time on the main flight row (usually two: dep and arr)
		if len(matches) > 0 {
			dep := matches[0][1]
			arr := ""
			if len(matches) > 1 {
				arr = matches[1][1]
			}
			
			// Normalize times: "10.25" or "10 25" -> "10:25"
			normalizeTime := func(t string) string {
				t = strings.ReplaceAll(t, ".", ":")
				t = strings.ReplaceAll(t, " ", ":")
				return t
			}
			dep = normalizeTime(dep)
			arr = normalizeTime(arr)

			// RECOVERY: Validate time components (Prevent 0895 or 1081)
			isValidTime := func(t string) bool {
				parts := strings.Split(t, ":")
				if len(parts) != 2 { return false }
				h, _ := strconv.Atoi(parts[0])
				m, _ := strconv.Atoi(parts[1])
				return h >= 0 && h < 24 && m >= 0 && m < 60
			}
			if !isValidTime(dep) {
				continue
			}

			flight := FlightInfo{
				DepartureTime: dep, ArrivalTime: arr,
				Airline: "Unknown", Price: "Unknown", Duration: "Unknown", Stops: "Unknown",
				DepartureAirport: "Unknown", ArrivalAirport: "Unknown",
			}

			// Look ahead up to 12 lines to find flight details.
			start, end := i, i+12
			if start < 0 { start = 0 }
			if end > len(lines) { end = len(lines) }

			normalizePrice := func(sym, val string) string {
				if sym == "E" || sym == "EUR" { sym = "€" }
				if sym == "GBP" { sym = "£" }
				if sym == "" { sym = "€" } // Default to Euro if missing but digit found
				return sym + val
			}

			// Priority 1: Check same line for price (most accurate)
			if pm := priceRegex.FindStringSubmatch(line); len(pm) > 0 {
				symbol, val := pm[1], strings.ReplaceAll(pm[2], ",", "")
				if p := parsePrice(val); p > 10 && (maxPrice <= 0 || p < maxPrice) {
					flight.Price = normalizePrice(symbol, val)
				}
			}

			// Priority 2: Check surrounding lines
			for j := start; j < end; j++ {
				l := lines[j]
				
				// RECOVERY: Only stop if we see a likely NEW flight starting (time match on a much later line)
				if j > i+3 && timeRegex.MatchString(l) {
					break
				}
				
				if pm := priceRegex.FindStringSubmatch(l); len(pm) > 0 && flight.Price == "Unknown" {
					symbol, val := pm[1], strings.ReplaceAll(pm[2], ",", "")
					if p := parsePrice(val); p > 10 && (maxPrice <= 0 || p < maxPrice) {
						flight.Price = normalizePrice(symbol, val)
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

func MinerExtractFromText(text string, opts SearchOptions) ([]FlightInfo, error) {
    return MinerExtractFlights(text, opts)
}
