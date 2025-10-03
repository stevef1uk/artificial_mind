package main

import (
	"fmt"
	"runtime"
)

func main() {
	fmt.Printf("Current architecture: %s\n", runtime.GOARCH)

	// Test the platform check logic
	if runtime.GOARCH == "arm64" {
		fmt.Println("✅ ARM64 detected - drone executor tool SHOULD be available")
	} else {
		fmt.Println("❌ Non-ARM64 detected - drone executor tool should NOT be available")
	}

	// Simulate the tool registration logic
	fmt.Println("\nTesting tool registration logic:")

	// This is what happens in BootstrapSeedTools
	if runtime.GOARCH == "arm64" {
		fmt.Println("✅ Registering ARM64-specific tools (including drone executor)")
	} else {
		fmt.Println("❌ Skipping ARM64-specific tools - not on ARM64 platform")
	}

	// This is what happens in handleInvokeTool
	fmt.Println("\nTesting tool invocation logic:")
	if runtime.GOARCH != "arm64" {
		fmt.Println("❌ Tool not available on this platform (X86)")
	} else {
		fmt.Println("✅ Tool available - proceeding with execution")
	}
}
