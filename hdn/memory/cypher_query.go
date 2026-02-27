//go:build neo4j
// +build neo4j

package memory

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// ExecuteCypher executes a Cypher query against Neo4j and returns a slice of row maps.
func ExecuteCypher(ctx context.Context, uri, user, pass, query string) ([]map[string]any, error) {
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, pass, ""))
	if err != nil {
		return nil, fmt.Errorf("neo4j connect failed: %w", err)
	}
	defer driver.Close(ctx)

	sess := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer sess.Close(ctx)

	rows := make([]map[string]any, 0)
	_, err = sess.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		res, err := tx.Run(ctx, query, map[string]any{})
		if err != nil {
			return nil, err
		}
		count := 0
		maxRows := 5000 // Global safety limit
		for res.Next(ctx) {
			count++
			if count > maxRows {
				break
			}
			record := res.Record()
			// Use AsMap() for convenience in v5 driver
			row := record.AsMap()
			rows = append(rows, row)
		}
		return nil, res.Err()
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}
