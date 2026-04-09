package main

type FlightInfo struct {
	Price            string `json:"price"`
	Airline          string `json:"airline"`
	Duration         string `json:"duration"`
	Stops            string `json:"stops"`
	DepartureTime    string `json:"departure_time"`
	ArrivalTime      string `json:"arrival_time"`
	DepartureAirport string `json:"departure_airport"`
	ArrivalAirport   string `json:"arrival_airport"`
	URL              string `json:"url"`
	CabinClass       string `json:"cabin_class"`
}

type SearchOptions struct {
	Departure   string
	Destination string
	StartDate   string
	EndDate     string
	CabinClass  string
	Language    string
	Region      string
	Currency    string
	Headless    bool
	BrowserPath string
}

type ScrapeJob struct {
	ID     string                 `json:"id"`
	Status string                 `json:"status"`
	Result map[string]interface{} `json:"result,omitempty"`
	Error  string                 `json:"error,omitempty"`
}
