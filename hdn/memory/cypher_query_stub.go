//go:build !neo4j
// +build !neo4j

package memory

import (
	"context"
	"fmt"
)

// ExecuteCypher is a stub when Neo4j build tag is not enabled.
func ExecuteCypher(ctx context.Context, uri, user, pass, query string) ([]map[string]any, error) {
	return nil, fmt.Errorf("ExecuteCypher requires Neo4j build tag")
}
