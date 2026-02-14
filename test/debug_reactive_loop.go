package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Simplified version of the loop to debug
func main() {
	url := "https://co2.myclimate.org/en/flight_calculators/new"

	projectRoot, _ := os.Getwd()
	browserBin := filepath.Join(projectRoot, "bin", "headless-browser")
	timeout := 60

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var accumulatedActions []map[string]interface{}

	step := 1
	log.Printf("🔄 Step %d", step)

	actionsJSON, _ := json.Marshal(accumulatedActions)
	args := []string{
		"-url", url,
		"-actions", string(actionsJSON),
		"-timeout", fmt.Sprintf("%d", timeout),
		"-html",
	}

	cmd := exec.CommandContext(ctx, browserBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("❌ Command failed: %v\nOutput: %s", err, string(output))
	}

	log.Printf("✅ Success! Output length: %d", len(output))

	// Try to find the JSON
	resultPattern := `{"success"`
	idx := strings.LastIndex(string(output), resultPattern)
	if idx == -1 {
		fmt.Printf("DEBUG: LAST 1000 CHARS OF OUTPUT:\n%s\n", string(output[max(0, len(output)-1000):]))
		log.Fatalf("❌ JSON result not found in output")
	}

	log.Printf("🔍 Found JSON start at index %d", idx)
}
