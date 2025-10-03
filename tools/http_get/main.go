package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"
)

type Result struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

func main() {
	url := flag.String("url", "", "URL to fetch")
	flag.Parse()
	if *url == "" {
		fmt.Fprintln(os.Stderr, "missing -url")
		os.Exit(2)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(*url)
	if err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(Result{Status: 0, Body: err.Error()})
		return
	}
	defer resp.Body.Close()
	b, _ := ioutil.ReadAll(resp.Body)
	_ = json.NewEncoder(os.Stdout).Encode(Result{Status: resp.StatusCode, Body: string(b)})
}
