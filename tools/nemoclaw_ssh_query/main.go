package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Result struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

func main() {
	prompt := flag.String("prompt", "", "Strategic prompt for NemoClaw")
	flag.Parse()

	if *prompt == "" {
		fmt.Fprintln(os.Stderr, "missing -prompt")
		os.Exit(2)
	}

	// Escape single quotes in prompt for shell
	escapedPrompt := strings.ReplaceAll(*prompt, "'", "'\\''")
	
	// Command to execute inside the connect shell
	// We use the exact command structure the user verified, now with session-id test1
	innerCmd := fmt.Sprintf("openclaw agent --agent main --local -m '%s' --session-id test1", escapedPrompt)
	
	// Full shell command to run on the target host
	// Using printf and an explicit exit to ensure the session closes after the command
	sshCmdStr := fmt.Sprintf("printf \"%%s\\nexit\\n\" \"%s\" | /home/stevef/.npm-global/bin/nemoclaw my-assistant connect", innerCmd)
	
	// Execute via SSH to the Omen machine (192.168.1.53) with a 2-minute timeout
	ctx, cancel := context.WithTimeout(context.Background(), 120 * time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=no", "-T", "stevef@192.168.1.53", sshCmdStr)
	
	out, err := cmd.CombinedOutput()
	
	result := Result{}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			result.Error = fmt.Sprintf("SSH command failed: %v, Stderr: %s", err, string(ee.Stderr))
		} else {
			result.Error = fmt.Sprintf("Failed to execute SSH: %v", err)
		}
	} else {
		// Return the raw output for now; HDN can handle the summary
		result.Response = string(out)
	}

	_ = json.NewEncoder(os.Stdout).Encode(result)
}
