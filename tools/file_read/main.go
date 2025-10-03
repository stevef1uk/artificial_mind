package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func main() {
	path := flag.String("path", "", "file path")
	flag.Parse()
	if *path == "" {
		fmt.Fprintln(os.Stderr, "missing -path")
		os.Exit(2)
	}
	b, err := os.ReadFile(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"content": string(b)})
}
