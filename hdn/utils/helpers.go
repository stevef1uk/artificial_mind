package utils

import (
	"fmt"
	"strings"
)

// TruncateString limits the size of a string to prevent OOM
func TruncateString(s string, limit int) string {
	if len(s) > limit {
		return s[:limit] + "... [TRUNCATED]"
	}
	return s
}

// SafeResultSummary creates a limited string representation of any object without OOM-ing.
// This is critical for preventing memory spikes during logging or trace storage of large research results.
func SafeResultSummary(v interface{}, limit int) string {
	if v == nil {
		return "nil"
	}
	switch val := v.(type) {
	case string:
		return TruncateString(val, limit)
	case []byte:
		return TruncateString(string(val), limit)
	case int, int32, int64, float32, float64, bool:
		return fmt.Sprintf("%v", val)
	case map[string]interface{}:
		return summarizeMap(val, limit)
	case map[string]string:
		// Convert map[string]string to map[string]interface{} for summarization
		m := make(map[string]interface{}, len(val))
		for k, v := range val {
			m[k] = v
		}
		return summarizeMap(m, limit)
	case []interface{}:
		if len(val) == 0 {
			return "[]"
		}
		// Show first item summary
		first := SafeResultSummary(val[0], limit/5)
		return fmt.Sprintf("List[%d]: [%s...]", len(val), first)
	case []string:
		if len(val) == 0 {
			return "[]"
		}
		return fmt.Sprintf("List[%d]: [%s...]", len(val), TruncateString(val[0], limit/5))
	default:
		// Safe fallback for other types to avoid building massive strings with %v
		return fmt.Sprintf("Object of type %T", v)
	}
}

func summarizeMap(m map[string]interface{}, limit int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
		if len(keys) >= 20 {
			break
		}
	}
	summary := fmt.Sprintf("Map with %d keys: [%s]", len(m), strings.Join(keys, ", "))
	if len(m) > 20 {
		summary += " ..."
	}
	return summary
}
