package playwright

import (
	"strings"
)

// Operation represents a parsed operation from TypeScript/Playwright code
type Operation struct {
	Type     string // "goto", "click", "fill", "getByRole", "getByText", "locator", "wait", "extract"
	Selector string // CSS selector or locator
	Value    string // For fill operations
	Role     string // For getByRole
	RoleName string // Name for getByRole
	Text     string // For getByText
	Timeout  int    // Timeout in seconds
}

// ParseTypeScript extracts operations from TypeScript/Playwright test code
func ParseTypeScript(tsConfig, defaultURL string) ([]Operation, error) {
	var operations []Operation

	lines := strings.Split(tsConfig, "\n")

	// Extract URL from page.goto if present, otherwise use defaultURL
	currentURL := defaultURL

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Parse page.goto
		if strings.Contains(line, "page.goto") {
			// Extract URL from page.goto('url') or page.goto("url")
			if idx := strings.Index(line, "goto("); idx != -1 {
				urlStart := idx + 5
				// Skip whitespace
				for urlStart < len(line) && (line[urlStart] == ' ' || line[urlStart] == '\t') {
					urlStart++
				}
				if urlStart < len(line) && (line[urlStart] == '\'' || line[urlStart] == '"') {
					quote := line[urlStart]
					urlStart++ // Skip opening quote
					urlEnd := urlStart
					// Find closing quote (same type as opening)
					for urlEnd < len(line) && line[urlEnd] != quote {
						urlEnd++
					}
					if urlStart <= urlEnd && urlEnd < len(line) {
						currentURL = line[urlStart:urlEnd]
					}
				}
			}
			operations = append(operations, Operation{Type: "goto", Selector: currentURL})
			continue
		}

		// Parse page.getByRole('link', { name: 'Plane' }).click()
		if strings.Contains(line, "getByRole") && strings.Contains(line, "click()") {
			// Extract role and name
			role := ""
			name := ""
			if idx := strings.Index(line, "getByRole("); idx != -1 {
				roleStart := idx + 10
				// Skip whitespace and opening quote
				for roleStart < len(line) && (line[roleStart] == ' ' || line[roleStart] == '\t') {
					roleStart++
				}
				if roleStart < len(line) && (line[roleStart] == '\'' || line[roleStart] == '"') {
					quote := line[roleStart]
					roleStart++ // Skip opening quote
					roleEnd := roleStart
					// Find closing quote
					for roleEnd < len(line) && line[roleEnd] != quote {
						roleEnd++
					}
					if roleStart < roleEnd {
						role = line[roleStart:roleEnd]
					}
				}
				// Find name: 'value'
				if nameIdx := strings.Index(line, "name:"); nameIdx != -1 {
						nameStart := nameIdx + 5
						for nameStart < len(line) && (line[nameStart] == ' ' || line[nameStart] == '\'' || line[nameStart] == '"') {
							nameStart++
						}
						nameEnd := nameStart
						for nameEnd < len(line) && line[nameEnd] != '\'' && line[nameEnd] != '"' && line[nameEnd] != ',' {
							nameEnd++
						}
						if nameStart < nameEnd {
							name = line[nameStart:nameEnd]
						}
					}
				}
			operations = append(operations, Operation{
				Type:     "getByRole",
				Role:     role,
				RoleName: name,
			})
			continue
		}

		// Parse page.getByRole('textbox', { name: 'From To Via' }).fill('southampton')
		if strings.Contains(line, "getByRole") && strings.Contains(line, "fill(") {
			role := ""
			name := ""
			value := ""
			if idx := strings.Index(line, "getByRole("); idx != -1 {
				roleStart := idx + 10
				// Skip whitespace and opening quote
				for roleStart < len(line) && (line[roleStart] == ' ' || line[roleStart] == '\t') {
					roleStart++
				}
				if roleStart < len(line) && (line[roleStart] == '\'' || line[roleStart] == '"') {
					quote := line[roleStart]
					roleStart++ // Skip opening quote
					roleEnd := roleStart
					// Find closing quote
					for roleEnd < len(line) && line[roleEnd] != quote {
						roleEnd++
					}
					if roleStart < roleEnd {
						role = line[roleStart:roleEnd]
					}
				}
				// Find name: 'value'
				if nameIdx := strings.Index(line, "name:"); nameIdx != -1 {
					nameStart := nameIdx + 5
					for nameStart < len(line) && (line[nameStart] == ' ' || line[nameStart] == '\'' || line[nameStart] == '"') {
						nameStart++
					}
					nameEnd := nameStart
					for nameEnd < len(line) && line[nameEnd] != '\'' && line[nameEnd] != '"' && line[nameEnd] != ',' {
						nameEnd++
					}
					if nameStart < nameEnd {
						name = line[nameStart:nameEnd]
					}
				}
			}
			// Extract fill value
			if fillIdx := strings.Index(line, "fill("); fillIdx != -1 {
				valueStart := fillIdx + 5
				for valueStart < len(line) && (line[valueStart] == ' ' || line[valueStart] == '\'' || line[valueStart] == '"') {
					valueStart++
				}
				valueEnd := valueStart
				for valueEnd < len(line) && line[valueEnd] != '\'' && line[valueEnd] != '"' && line[valueEnd] != ')' {
					valueEnd++
				}
				if valueStart < valueEnd {
					value = line[valueStart:valueEnd]
				}
			}
			operations = append(operations, Operation{
				Type:     "getByRoleFill",
				Role:     role,
				RoleName: name,
				Value:    value,
			})
			continue
		}

		// Parse page.getByText('Southampton, United Kingdom').click()
		if strings.Contains(line, "getByText") && strings.Contains(line, "click()") {
			text := ""
			if idx := strings.Index(line, "getByText("); idx != -1 {
				textStart := idx + 10
				for textStart < len(line) && (line[textStart] == ' ' || line[textStart] == '\'' || line[textStart] == '"') {
					textStart++
				}
				textEnd := textStart
				for textEnd < len(line) && line[textEnd] != '\'' && line[textEnd] != '"' && line[textEnd] != ')' {
					textEnd++
				}
				if textStart < textEnd {
					text = line[textStart:textEnd]
				}
			}
			operations = append(operations, Operation{
				Type: "getByText",
				Text: text,
			})
			continue
		}

		// Parse page.locator('input[name="To"]').click()
		if strings.Contains(line, "locator(") && strings.Contains(line, "click()") {
			selector := ""
			if idx := strings.Index(line, "locator("); idx != -1 {
				selStart := idx + 8
				// Skip whitespace
				for selStart < len(line) && (line[selStart] == ' ' || line[selStart] == '\t') {
					selStart++
				}
				// Find the opening quote
				if selStart < len(line) && (line[selStart] == '\'' || line[selStart] == '"') {
					quote := line[selStart]
					selStart++ // Skip opening quote
					selEnd := selStart
					// Find closing quote, handling escaped quotes
					for selEnd < len(line) {
						if line[selEnd] == '\\' && selEnd+1 < len(line) {
							selEnd += 2 // Skip escaped character
							continue
						}
						if line[selEnd] == quote {
							break
						}
						selEnd++
					}
					if selStart < selEnd {
						selector = line[selStart:selEnd]
					}
				}
			}
			operations = append(operations, Operation{
				Type:     "locator",
				Selector: selector,
			})
			continue
		}

		// Parse page.locator('input[name="To"]').fill('newcastle')
		if strings.Contains(line, "locator(") && strings.Contains(line, "fill(") {
			selector := ""
			value := ""
			if idx := strings.Index(line, "locator("); idx != -1 {
				selStart := idx + 8
				// Skip whitespace
				for selStart < len(line) && (line[selStart] == ' ' || line[selStart] == '\t') {
					selStart++
				}
				// Find the opening quote
				if selStart < len(line) && (line[selStart] == '\'' || line[selStart] == '"') {
					quote := line[selStart]
					selStart++ // Skip opening quote
					selEnd := selStart
					// Find closing quote, handling escaped quotes
					for selEnd < len(line) {
						if line[selEnd] == '\\' && selEnd+1 < len(line) {
							selEnd += 2 // Skip escaped character
							continue
						}
						if line[selEnd] == quote {
							break
						}
						selEnd++
					}
					if selStart < selEnd {
						selector = line[selStart:selEnd]
					}
				}
			}
			if fillIdx := strings.Index(line, "fill("); fillIdx != -1 {
				valueStart := fillIdx + 5
				// Skip whitespace
				for valueStart < len(line) && (line[valueStart] == ' ' || line[valueStart] == '\t') {
					valueStart++
				}
				// Find the opening quote
				if valueStart < len(line) && (line[valueStart] == '\'' || line[valueStart] == '"') {
					quote := line[valueStart]
					valueStart++ // Skip opening quote
					valueEnd := valueStart
					// Find closing quote
					for valueEnd < len(line) && line[valueEnd] != quote {
						valueEnd++
					}
					if valueStart < valueEnd {
						value = line[valueStart:valueEnd]
					}
				}
			}
			operations = append(operations, Operation{
				Type:     "locatorFill",
				Selector: selector,
				Value:    value,
			})
			continue
		}
	}

	return operations, nil
}

