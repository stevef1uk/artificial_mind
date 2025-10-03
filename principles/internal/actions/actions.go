package actions

import "fmt"

func PerformExampleAction(params map[string]interface{}) string {
	return fmt.Sprintf("Action performed with params: %v", params)
}

