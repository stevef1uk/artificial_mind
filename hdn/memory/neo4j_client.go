//go:build neo4j
// +build neo4j

package memory

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// SemanticKB provides simple CRUD operations over Neo4j for entities and relations.
type SemanticKB struct {
	driver neo4j.DriverWithContext
}

func NewSemanticKB(uri, user, pass string) (*SemanticKB, error) {
	drv, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, pass, ""))
	if err != nil {
		return nil, err
	}
	return &SemanticKB{driver: drv}, nil
}

func (kb *SemanticKB) Close(ctx context.Context) error {
	return kb.driver.Close(ctx)
}

// UpsertEntity creates or updates an entity node with labels and properties.
func (kb *SemanticKB) UpsertEntity(ctx context.Context, label string, id string, props map[string]interface{}) error {
	sess := kb.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer sess.Close(ctx)

	_, err := sess.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := fmt.Sprintf("MERGE (n:%s {id: $id}) SET n += $props RETURN n", label)
		_, err := tx.Run(ctx, cypher, map[string]any{"id": id, "props": props})
		return nil, err
	})
	return err
}

// CreateRelation creates/updates a relation with type relType between two entities.
func (kb *SemanticKB) CreateRelation(ctx context.Context, srcLabel, srcID, relType, dstLabel, dstID string, props map[string]any) error {
	sess := kb.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer sess.Close(ctx)

	_, err := sess.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := fmt.Sprintf("MERGE (a:%s {id: $src}) MERGE (b:%s {id: $dst}) MERGE (a)-[r:%s]->(b) SET r += $props RETURN r", srcLabel, dstLabel, relType)
		_, err := tx.Run(ctx, cypher, map[string]any{"src": srcID, "dst": dstID, "props": props})
		return nil, err
	})
	return err
}

// GetEntity returns properties of an entity.
func (kb *SemanticKB) GetEntity(ctx context.Context, label, id string) (map[string]any, error) {
	sess := kb.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer sess.Close(ctx)

	res, err := sess.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := fmt.Sprintf("MATCH (n:%s {id: $id}) RETURN n", label)
		rec, err := tx.Run(ctx, cypher, map[string]any{"id": id})
		if err != nil {
			return nil, err
		}
		if rec.Next(ctx) {
			node, _ := rec.Record().Get("n")
			if n, ok := node.(neo4j.Node); ok {
				props := n.Props
				return props, nil
			}
		}
		return map[string]any{}, rec.Err()
	})
	if err != nil {
		return nil, err
	}
	return res.(map[string]any), nil
}
