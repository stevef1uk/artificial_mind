package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: ./test_audio <wav_file>")
		os.Exit(1)
	}

	filePath := os.Args[1]
	cardIndex := os.Getenv("SOUND_CARD_INDEX")
	if cardIndex == "" {
		cardIndex = "2" // default for user
	}
	device := "plughw:" + cardIndex + ",0"

	fmt.Printf("Testing audio playback on card %s (device %s)\n", cardIndex, device)
	fmt.Printf("File: %s\n", filePath)

	// Test 1: play (SoX)
	fmt.Println("\n--- Testing 'play' (SoX) ---")
	cmd := exec.Command("play", "-q", "-D", device, filePath)
	err := cmd.Run()
	if err != nil {
		fmt.Printf("play failed: %v\n", err)
	} else {
		fmt.Println("play succeeded!")
	}

	// Test 2: aplay
	fmt.Println("\n--- Testing 'aplay' ---")
	cmd = exec.Command("aplay", "-D", device, filePath)
	err = cmd.Run()
	if err != nil {
		fmt.Printf("aplay failed: %v\n", err)
	} else {
		fmt.Println("aplay succeeded!")
	}

	// Test 3: speaker-test (just to hear noise)
	fmt.Println("\n--- Testing 'speaker-test' (2 seconds of noise) ---")
	cmd = exec.Command("speaker-test", "-c1", "-t", "pink", "-w", "2", "-D", device)
	cmd.Run()
}
