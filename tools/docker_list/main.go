package main

import (
	"encoding/json"
	"flag"
	"os"
)

// Stub implementation; real one would use Docker SDK
func main() {
	typ := flag.String("type", "containers", "containers|images")
	flag.Parse()
	_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"type": *typ, "items": []string{}})
}
