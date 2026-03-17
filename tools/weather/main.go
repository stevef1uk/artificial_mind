package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// ─────────────────────────────────────────────
// API response structs
// ─────────────────────────────────────────────

type CurrentUnits struct {
	Temperature2m string `json:"temperature_2m"`
	WindSpeed10m  string `json:"wind_speed_10m"`
	Precipitation string `json:"precipitation"`
	Rain          string `json:"rain"`
	Snowfall      string `json:"snowfall"`
	WeatherCode   string `json:"weather_code"`
}

type Current struct {
	Time          string  `json:"time"`
	Temperature2m float64 `json:"temperature_2m"`
	WindSpeed10m  float64 `json:"wind_speed_10m"`
	Precipitation float64 `json:"precipitation"`
	Rain          float64 `json:"rain"`
	Snowfall      float64 `json:"snowfall"`
	WeatherCode   int     `json:"weather_code"`
}

type HourlyData struct {
	Time          []string  `json:"time"`
	Temperature2m []float64 `json:"temperature_2m"`
	Precipitation []float64 `json:"precipitation"`
	Rain          []float64 `json:"rain"`
	Snowfall      []float64 `json:"snowfall"`
}

type DailyData struct {
	Time             []string  `json:"time"`
	WeatherCode      []int     `json:"weather_code"`
	Temperature2mMax []float64 `json:"temperature_2m_max"`
	Temperature2mMin []float64 `json:"temperature_2m_min"`
	WindSpeed10mMax  []float64 `json:"wind_speed_10m_max"`
	RainSum          []float64 `json:"rain_sum"`
	ShowersSum       []float64 `json:"showers_sum"`
	SnowfallSum      []float64 `json:"snowfall_sum"`
	PrecipitationSum []float64 `json:"precipitation_sum"`
}

type APIResponse struct {
	Latitude     float64      `json:"latitude"`
	Longitude    float64      `json:"longitude"`
	Elevation    float64      `json:"elevation"`
	Timezone     string       `json:"timezone"`
	Current      Current      `json:"current"`
	CurrentUnits CurrentUnits `json:"current_units"`
	Hourly       HourlyData   `json:"hourly"`
	Daily        DailyData    `json:"daily"`
}

// ─────────────────────────────────────────────
// Output struct
// ─────────────────────────────────────────────

type PrecipSummary struct {
	RainMm     float64 `json:"rain_mm"`
	ShowersMm  float64 `json:"showers_mm"`
	SnowfallCm float64 `json:"snowfall_cm"`
	TotalMm    float64 `json:"total_mm"`
}

type WeatherSummary struct {
	Location struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Elevation float64 `json:"elevation_m"`
		Timezone  string  `json:"timezone"`
	} `json:"location"`

	Current struct {
		Time          string  `json:"time"`
		TemperatureC  float64 `json:"temperature_c"`
		WindSpeedKmh  float64 `json:"wind_speed_kmh"`
		Precipitation float64 `json:"precipitation_mm"`
		Rain          float64 `json:"rain_mm"`
		Snowfall      float64 `json:"snowfall_cm"`
		WeatherCode   int     `json:"weather_code"`
		WeatherDesc   string  `json:"weather_description"`
	} `json:"current"`

	DailyStats struct {
		Date            string        `json:"date"`
		TemperatureMinC float64       `json:"temperature_min_c"`
		TemperatureMaxC float64       `json:"temperature_max_c"`
		WindSpeedMaxKmh float64       `json:"wind_speed_max_kmh"`
		Precipitation   PrecipSummary `json:"precipitation"`
		WeatherCode     int           `json:"weather_code"`
		WeatherDesc     string        `json:"weather_description"`
	} `json:"daily_stats"`

	HourlyRange struct {
		TemperatureMinC float64 `json:"temperature_min_c"`
		TemperatureMaxC float64 `json:"temperature_max_c"`
		PrecipMaxHourMm float64 `json:"precip_max_hour_mm"`
	} `json:"hourly_range"`
}

// ─────────────────────────────────────────────
// WMO weather code descriptions
// ─────────────────────────────────────────────

func wmoDescription(code int) string {
	descriptions := map[int]string{
		0:  "Clear sky",
		1:  "Mainly clear",
		2:  "Partly cloudy",
		3:  "Overcast",
		45: "Fog",
		48: "Depositing rime fog",
		51: "Light drizzle",
		53: "Moderate drizzle",
		55: "Dense drizzle",
		56: "Light freezing drizzle",
		57: "Heavy freezing drizzle",
		61: "Slight rain",
		63: "Moderate rain",
		65: "Heavy rain",
		66: "Light freezing rain",
		67: "Heavy freezing rain",
		71: "Slight snowfall",
		73: "Moderate snowfall",
		75: "Heavy snowfall",
		77: "Snow grains",
		80: "Slight rain showers",
		81: "Moderate rain showers",
		82: "Violent rain showers",
		85: "Slight snow showers",
		86: "Heavy snow showers",
		95: "Thunderstorm",
		96: "Thunderstorm with slight hail",
		99: "Thunderstorm with heavy hail",
	}
	if desc, ok := descriptions[code]; ok {
		return desc
	}
	return fmt.Sprintf("Unknown (code %d)", code)
}

// ─────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────

func minFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func maxFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

// ─────────────────────────────────────────────
// Core function
// ─────────────────────────────────────────────

// Timezone should be "Berlin" or "London" (we prepend "Europe/")
func FetchWeather(latitude, longitude float64, timezone string) (*WeatherSummary, error) {
	tz := "Europe/" + timezone

	params := url.Values{}
	params.Set("latitude", fmt.Sprintf("%f", latitude))
	params.Set("longitude", fmt.Sprintf("%f", longitude))
	params.Set("timezone", tz)
	params.Set("forecast_days", "1")
	params.Set("current", "temperature_2m,wind_speed_10m,precipitation,rain,snowfall,weather_code")
	params.Set("hourly", "temperature_2m,precipitation,rain,snowfall")
	params.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,wind_speed_10m_max,rain_sum,showers_sum,snowfall_sum,precipitation_sum")

	apiURL := "https://api.open-meteo.com/v1/forecast?" + params.Encode()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var raw APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// ── Build summary ──────────────────────────────
	s := &WeatherSummary{}

	s.Location.Latitude = raw.Latitude
	s.Location.Longitude = raw.Longitude
	s.Location.Elevation = raw.Elevation
	s.Location.Timezone = raw.Timezone

	// Current
	s.Current.Time = raw.Current.Time
	s.Current.TemperatureC = raw.Current.Temperature2m
	s.Current.WindSpeedKmh = raw.Current.WindSpeed10m
	s.Current.Precipitation = raw.Current.Precipitation
	s.Current.Rain = raw.Current.Rain
	s.Current.Snowfall = raw.Current.Snowfall
	s.Current.WeatherCode = raw.Current.WeatherCode
	s.Current.WeatherDesc = wmoDescription(raw.Current.WeatherCode)

	// Daily (index 0 = today)
	if len(raw.Daily.Time) > 0 {
		s.DailyStats.Date = raw.Daily.Time[0]
		s.DailyStats.WeatherCode = raw.Daily.WeatherCode[0]
		s.DailyStats.WeatherDesc = wmoDescription(raw.Daily.WeatherCode[0])
		s.DailyStats.TemperatureMinC = raw.Daily.Temperature2mMin[0]
		s.DailyStats.TemperatureMaxC = raw.Daily.Temperature2mMax[0]
		s.DailyStats.WindSpeedMaxKmh = raw.Daily.WindSpeed10mMax[0]
		s.DailyStats.Precipitation = PrecipSummary{
			RainMm:     raw.Daily.RainSum[0],
			ShowersMm:  raw.Daily.ShowersSum[0],
			SnowfallCm: raw.Daily.SnowfallSum[0],
			TotalMm:    raw.Daily.PrecipitationSum[0],
		}
	}

	// Hourly range
	s.HourlyRange.TemperatureMinC = minFloat(raw.Hourly.Temperature2m)
	s.HourlyRange.TemperatureMaxC = maxFloat(raw.Hourly.Temperature2m)
	s.HourlyRange.PrecipMaxHourMm = maxFloat(raw.Hourly.Precipitation)

	return s, nil
}

// ─────────────────────────────────────────────
// Main
// ─────────────────────────────────────────────

func main() {
	lat := flag.Float64("lat", 46.2623, "Latitude")
	lon := flag.Float64("lon", 6.6313, "Longitude")
	tz := flag.String("tz", "Berlin", "Timezone (Europe/ prepend automatically)")
	flag.Parse()

	summary, err := FetchWeather(*lat, *lon, *tz)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	out, err := json.Marshal(summary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "JSON marshal error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(out))
}
