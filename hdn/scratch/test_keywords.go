package main

import (
	"fmt"
	"strings"
)

type PromptHintsConfig struct {
	Keywords []string
}

var promptHintsRegistry = make(map[string]*PromptHintsConfig)

func Set_tool_generate_image_hints() {
	promptHintsRegistry["tool_generate_image"] = &PromptHintsConfig{
		Keywords: []string{"image", "create image", "generate image", "make image", "picture", "drawing", "illustration", "visual", "photo", "artwork", "change", "modify", "update", "edit", "background", "foreground", "style"},
	}
}

func MatchesConfiguredToolKeywords(message string) string {
	messageLower := strings.ToLower(message)
	for toolID, hints := range promptHintsRegistry {
		for _, keyword := range hints.Keywords {
			if strings.Contains(messageLower, strings.ToLower(keyword)) {
				return toolID
			}
		}
	}
	return ""
}

func main() {
	Set_tool_generate_image_hints()
	message := "create me a cute picture of a house"
	match := MatchesConfiguredToolKeywords(message)
	fmt.Printf("Message: %s\nMatch: %s\n", message, match)
}
