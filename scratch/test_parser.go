package main

import (
	"fmt"
	"log"
	"strings"
)

type PlaywrightOperation struct {
	Type   string
	Script string
}

func main() {
	tsConfig := `
            await page.evaluate(() => {
                console.log("DEBUG TITLE: " + document.title);
                console.log("DEBUG URL: " + window.location.href);
            });

            await page.evaluate(() => {
                const results = document.querySelectorAll("div[role='listitem'], li.pI9Vpc");
                console.log("DEBUG RESULTS: " + results.length);
            });
`
	lines := strings.Split(tsConfig, "\n")
	var operations []PlaywrightOperation
	var currentOp strings.Builder
	inEvaluate := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" { continue }
		
		if !inEvaluate && strings.Contains(trimmed, "page.evaluate") {
			fmt.Println("Matched START of evaluate:", trimmed)
			inEvaluate = true
			currentOp.Reset()
			currentOp.WriteString(line + "\n")
			// Single-line block check
			if (strings.HasSuffix(trimmed, "})") || strings.HasSuffix(trimmed, "});")) && strings.Contains(trimmed, "=>") {
				fmt.Println("Matched SINGLE-LINE evaluate")
				inEvaluate = false
				content := currentOp.String()
				startIdx := strings.Index(content, "{")
				endIdx := strings.LastIndex(content, "}")
				if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
					inner := strings.TrimSpace(content[startIdx+1 : endIdx])
					operations = append(operations, PlaywrightOperation{Type: "evaluate", Script: inner})
				}
				currentOp.Reset()
			}
			continue
		}
		
		if inEvaluate {
			fmt.Println("Matched INSIDE evaluate:", trimmed)
			currentOp.WriteString(line + "\n")
			if strings.Contains(trimmed, "})") || strings.Contains(trimmed, ");") {
				fmt.Println("Matched END of evaluate")
				inEvaluate = false
				content := currentOp.String()
				startIdx := strings.Index(content, "{")
				endIdx := strings.LastIndex(content, "}")
				if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
					inner := strings.TrimSpace(content[startIdx+1 : endIdx])
					operations = append(operations, PlaywrightOperation{Type: "evaluate", Script: inner})
				} else {
					log.Printf("⚠️ Failed to parse evaluate block: %s", trimmed)
				}
				currentOp.Reset()
			}
			continue
		}
	}

	for i, op := range operations {
		fmt.Printf("OP %d TYPE: %s\n", i, op.Type)
		fmt.Printf("OP %d SCRIPT:\n%s\n---\n", i, op.Script)
	}
}
