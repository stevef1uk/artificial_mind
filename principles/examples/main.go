package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go [basic|dynamic]")
		fmt.Println("  basic   - Run basic HDN integration example")
		fmt.Println("  dynamic - Run dynamic LLM integration example")
		return
	}

	switch os.Args[1] {
	case "basic":
		mainBasic()
	case "dynamic":
		mainDynamic()
	default:
		fmt.Printf("Unknown option: %s\n", os.Args[1])
		fmt.Println("Use 'basic' or 'dynamic'")
	}
}
