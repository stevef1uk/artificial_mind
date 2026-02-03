package main

import (
	"encoding/json"
	"fmt"
	"os"
	
	// Note: This uses the replace directive in go.mod to find hdn module
	"agi/hdn/playwright"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <typescript-file>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s /home/stevef/flight-test.ts\n", os.Args[0])
		os.Exit(1)
	}

	tsFile := os.Args[1]
	tsConfig, err := os.ReadFile(tsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("🧪 Testing TypeScript/Playwright Parser\n")
	fmt.Printf("=====================================\n")
	fmt.Printf("File: %s\n", tsFile)
	fmt.Printf("Size: %d bytes\n\n", len(tsConfig))

	// Parse the TypeScript using the shared parser
	operations, err := playwright.ParseTypeScript(string(tsConfig), "https://example.com")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing TypeScript: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Parsed %d operations\n\n", len(operations))

	// Print operations in a readable format
	for i, op := range operations {
		fmt.Printf("Operation %d:\n", i+1)
		fmt.Printf("  Type: %s\n", op.Type)
		if op.Selector != "" {
			fmt.Printf("  Selector: %s\n", op.Selector)
		}
		if op.Value != "" {
			fmt.Printf("  Value: %s\n", op.Value)
		}
		if op.Role != "" {
			fmt.Printf("  Role: %s\n", op.Role)
		}
		if op.RoleName != "" {
			fmt.Printf("  Role Name: %s\n", op.RoleName)
		}
		if op.Text != "" {
			fmt.Printf("  Text: %s\n", op.Text)
		}
		fmt.Println()
	}

	// Also print as JSON for programmatic use
	fmt.Println("JSON Output:")
	jsonOutput, err := json.MarshalIndent(operations, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(jsonOutput))
}
