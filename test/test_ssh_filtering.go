package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Test SSH message filtering logic
func main() {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "SSH warning only",
			input:    "Warning: Permanently added '192.168.1.63' (ED25519) to the list of known hosts.",
			expected: "",
		},
		{
			name:     "SSH warning with actual output",
			input:    "Warning: Permanently added '192.168.1.63' (ED25519) to the list of known hosts.\n[2, 3, 5, 7, 11, 13, 17, 19, 23, 29]",
			expected: "[2, 3, 5, 7, 11, 13, 17, 19, 23, 29]",
		},
		{
			name:     "SSH warning in middle",
			input:    "[2, 3, 5]\nWarning: Permanently added '192.168.1.63' (ED25519) to the list of known hosts.\n[7, 11, 13]",
			expected: "[2, 3, 5]\n[7, 11, 13]",
		},
		{
			name:     "Normal output without SSH",
			input:    "[2, 3, 5, 7, 11, 13, 17, 19, 23, 29]",
			expected: "[2, 3, 5, 7, 11, 13, 17, 19, 23, 29]",
		},
		{
			name:     "SSH warning with Go matrix output",
			input:    "Warning: Permanently added '192.168.1.63' (ED25519) to the list of known hosts.\n[6 8]\n[10 12]",
			expected: "[6 8]\n[10 12]",
		},
		{
			name:     "Multiple SSH warnings",
			input:    "Warning: Permanently added '192.168.1.63' (ED25519) to the list of known hosts.\nWarning: Permanently added '192.168.1.64' (ED25519) to the list of known hosts.\n[2, 3, 5]",
			expected: "[2, 3, 5]",
		},
	}

	sshMessagePattern := regexp.MustCompile(`(?i).*(Warning: Permanently added|The authenticity of host|Host key verification failed|Warning:.*known hosts).*`)

	passed := 0
	failed := 0

	for _, tc := range testCases {
		result := filterSSHMessages(tc.input, sshMessagePattern)
		if result == tc.expected {
			fmt.Printf("✅ PASS: %s\n", tc.name)
			passed++
		} else {
			fmt.Printf("❌ FAIL: %s\n", tc.name)
			fmt.Printf("   Input:    %q\n", tc.input)
			fmt.Printf("   Expected: %q\n", tc.expected)
			fmt.Printf("   Got:      %q\n", result)
			failed++
		}
	}

	fmt.Printf("\nResults: %d passed, %d failed\n", passed, failed)
	if failed > 0 {
		fmt.Println("\n⚠️  Some tests failed - filtering logic needs adjustment")
	} else {
		fmt.Println("\n✅ All tests passed!")
	}
}

func filterSSHMessages(output string, sshMessagePattern *regexp.Regexp) string {
	cleanOutput := output

	// Split into lines and filter
	lines := strings.Split(cleanOutput, "\n")
	filteredLines := []string{}

	for _, line := range lines {
		lineTrimmed := strings.TrimSpace(line)
		if lineTrimmed != "" && !sshMessagePattern.MatchString(lineTrimmed) && !strings.HasPrefix(lineTrimmed, "Warning: Permanently added") {
			filteredLines = append(filteredLines, line)
		}
	}

	cleanOutput = strings.Join(filteredLines, "\n")
	
	// Final pass: remove any remaining SSH messages
	cleanOutput = sshMessagePattern.ReplaceAllString(cleanOutput, "")
	cleanOutput = strings.TrimSpace(cleanOutput)

	// Final safety check: if output is ONLY SSH messages, treat as empty
	outputLines := strings.Split(cleanOutput, "\n")
	nonSSHLines := []string{}
	for _, line := range outputLines {
		lineTrimmed := strings.TrimSpace(line)
		if lineTrimmed != "" && !sshMessagePattern.MatchString(lineTrimmed) && !strings.HasPrefix(lineTrimmed, "Warning: Permanently added") {
			nonSSHLines = append(nonSSHLines, line)
		}
	}
	if len(nonSSHLines) == 0 && len(outputLines) > 0 {
		cleanOutput = ""
	} else {
		cleanOutput = strings.Join(nonSSHLines, "\n")
	}

	return cleanOutput
}










