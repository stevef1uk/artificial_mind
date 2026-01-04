package main

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// extractTimingFromOutput extracts algorithm execution time from program output
// Looks for patterns like "took: 123ns", "duration: 456ms", "Time: 789 microseconds", etc.
func extractTimingFromOutput(output string, language string) int64 {
	if output == "" {
		return 0
	}

	// Common timing patterns in output
	patterns := []*regexp.Regexp{
		// Go: "Execution time: 540 nanoseconds" - prioritize this specific format
		regexp.MustCompile(`(?i)execution\s+time[:\s]+(\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s*(ns|nanoseconds|nanosecond)`),
		// Go: "Execution time: 9m30s nanoseconds" - handle Go Duration strings
		regexp.MustCompile(`(?i)execution\s+time[:\s]+((?:\d+h)?(?:\d+m)?(?:\d+(?:\.\d+)?s)?)\s*(ns|nanoseconds|nanosecond|us|microseconds|microsecond|ms|milliseconds|millisecond|s|seconds|second)`),
		// Go: "took: 123456ns" or "Duration: 123456ns" or "took: 1.234567s"
		regexp.MustCompile(`(?i)(?:took|duration|time|elapsed)[:\s]+((?:\d+h)?(?:\d+m)?(?:\d+(?:\.\d+)?s)?|\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s*(ns|nanoseconds|nanosecond|us|microseconds|microsecond|ms|milliseconds|millisecond|s|seconds|second)`),
		// Python: "Execution time: 0.123456 seconds" or "Execution time: 5.0067901611328125e-06 seconds" (with scientific notation)
		regexp.MustCompile(`(?i)execution\s+time[:\s]+(\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s*(s|seconds|second|ms|milliseconds|millisecond|us|microseconds|microsecond|ns|nanoseconds|nanosecond)`),
		// Python: "took: 0.123456 seconds" or "duration: 123.456 ms" (with scientific notation)
		regexp.MustCompile(`(?i)(?:took|duration|time|elapsed)[:\s]+(\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s*(ms|milliseconds|millisecond|s|seconds|second|us|microseconds|microsecond|ns|nanoseconds|nanosecond)`),
		// Generic: "Execution time: 123456" or "Sorting time: 0.123" (with scientific notation)
		regexp.MustCompile(`(?i)(?:sorting\s+time|algorithm\s+time)[:\s]+(\d+(?:\.\d+)?(?:e[+-]?\d+)?)\s*(ns|nanoseconds|nanosecond|ms|milliseconds|millisecond|s|seconds|second|us|microseconds|microsecond)`),
	}

	// Try each pattern and find ALL matches, then use the LAST one
	// This handles cases where timing is printed multiple times (e.g., in loops)
	var lastMatch []string

	for _, pattern := range patterns {
		// Find all matches in the output
		allMatches := pattern.FindAllStringSubmatch(output, -1)
		if len(allMatches) > 0 {
			// Use the last match (most likely the final timing)
			match := allMatches[len(allMatches)-1]
			if len(match) >= 3 {
				lastMatch = match
			}
		}
	}

	// Process the last match found
	if lastMatch != nil && len(lastMatch) >= 3 {
		valueStr := lastMatch[1]
		unit := strings.ToLower(lastMatch[2])

		var nanoseconds int64

		// Check if this is a Go Duration string (e.g., "9m30s", "1h2m3s", "5s")
		// Duration strings have format like "9m30s", "1h2m3.5s", etc.
		if matched, _ := regexp.MatchString(`^(?:\d+h)?(?:\d+m)?(?:\d+(?:\.\d+)?s)?$`, valueStr); matched {
			// Parse Go Duration string using time.ParseDuration
			if duration, err := time.ParseDuration(valueStr); err == nil {
				nanoseconds = duration.Nanoseconds()
				log.Printf("🔍 [TIMING-EXTRACT] Parsed Go Duration string '%s' = %d ns", valueStr, nanoseconds)
				if nanoseconds > 0 {
					return nanoseconds
				}
			} else {
				log.Printf("⚠️ [TIMING-EXTRACT] Failed to parse Go Duration string '%s': %v", valueStr, err)
			}
		}

		// Try parsing as a numeric value (handles scientific notation)
		var value float64
		// Use strconv.ParseFloat which handles both regular and scientific notation (e.g., 5.24e-06)
		if parsed, err := strconv.ParseFloat(valueStr, 64); err == nil {
			value = parsed
		} else {
			log.Printf("⚠️ [TIMING-EXTRACT] Failed to parse timing value '%s': %v", valueStr, err)
			return 0
		}

		// Convert to nanoseconds based on unit
		switch unit {
		case "ns", "nanoseconds", "nanosecond":
			nanoseconds = int64(value)
		case "us", "microseconds", "microsecond":
			nanoseconds = int64(value * 1000)
		case "ms", "milliseconds", "millisecond":
			nanoseconds = int64(value * 1000000)
		case "s", "seconds", "second":
			nanoseconds = int64(value * 1000000000)
		default:
			return 0
		}

		if nanoseconds > 0 {
			log.Printf("🔍 [TIMING-EXTRACT] Found timing: %s %s = %d ns (from last occurrence)", valueStr, unit, nanoseconds)
			return nanoseconds
		}
	}

	return 0
}

func main() {
	fmt.Println("Testing timing extraction logic...")
	fmt.Println(strings.Repeat("=", 70))

	testCases := []struct {
		name     string
		output   string
		language string
		expected int64
		desc     string
	}{
		{
			name:     "Go: Simple nanoseconds",
			output:   "Sorted array: [1 2 4 5 8]\nExecution time: 540 nanoseconds",
			language: "go",
			expected: 540,
			desc:     "Should extract 540 nanoseconds",
		},
		{
			name:     "Python: Scientific notation seconds",
			output:   "Execution time: 5.0067901611328125e-06 seconds\nSorted array: [1, 2, 4, 5, 8]",
			language: "python",
			expected: 5006, // 5.0067901611328125e-06 * 1e9 = ~5006 ns
			desc:     "Should extract scientific notation (5.006e-06 seconds)",
		},
		{
			name:     "Python: Decimal nanoseconds",
			output:   "Execution time: 2145.7672119140625 nanoseconds\nSorted array: [34, 64, 25, 12, 22, 11, 90]",
			language: "python",
			expected: 2145, // Should round to int64
			desc:     "Should extract decimal nanoseconds (2145.76... ns)",
		},
		{
			name:     "Go: Duration string",
			output:   "Execution time: 9m30s nanoseconds",
			language: "go",
			expected: 570000000000, // 9 minutes 30 seconds = 570 seconds = 570e9 ns
			desc:     "Should parse Go Duration string (9m30s)",
		},
		{
			name:     "Go: Duration string with hours",
			output:   "Execution time: 1h2m3s nanoseconds",
			language: "go",
			expected: 3723000000000, // 1 hour 2 minutes 3 seconds
			desc:     "Should parse Go Duration string with hours",
		},
		{
			name:     "Python: Regular seconds",
			output:   "Execution time: 0.000005 seconds",
			language: "python",
			expected: 5000, // 0.000005 * 1e9 = 5000 ns
			desc:     "Should extract regular decimal seconds",
		},
		{
			name:     "Go: Multiple timings (use last)",
			output:   "Iteration 1: Execution time: 100 nanoseconds\nIteration 2: Execution time: 200 nanoseconds\nFinal: Execution time: 540 nanoseconds",
			language: "go",
			expected: 540,
			desc:     "Should use the last timing when multiple are present",
		},
		{
			name:     "Python: Milliseconds",
			output:   "Execution time: 0.123 milliseconds",
			language: "python",
			expected: 123000, // 0.123 * 1e6 = 123000 ns
			desc:     "Should extract milliseconds",
		},
		{
			name:     "No timing found",
			output:   "Sorted array: [1, 2, 3]\nNo timing information",
			language: "python",
			expected: 0,
			desc:     "Should return 0 when no timing found",
		},
	}

	passed := 0
	failed := 0

	for i, tc := range testCases {
		fmt.Printf("\nTest %d: %s\n", i+1, tc.name)
		fmt.Printf("  Description: %s\n", tc.desc)
		fmt.Printf("  Input: %q\n", tc.output)
		
		result := extractTimingFromOutput(tc.output, tc.language)
		
		// Allow small tolerance for floating point conversions
		tolerance := int64(10) // 10 nanoseconds tolerance
		diff := result - tc.expected
		if diff < 0 {
			diff = -diff
		}
		
		if diff <= tolerance {
			fmt.Printf("  ✅ PASS: Expected ~%d ns, got %d ns\n", tc.expected, result)
			passed++
		} else {
			fmt.Printf("  ❌ FAIL: Expected ~%d ns, got %d ns (diff: %d ns)\n", tc.expected, result, diff)
			failed++
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Printf("Results: %d passed, %d failed\n", passed, failed)
	
	if failed > 0 {
		fmt.Println("❌ Some tests failed!")
		// Exit with error code
		// os.Exit(1)
	} else {
		fmt.Println("✅ All tests passed!")
	}
}




