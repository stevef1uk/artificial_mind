//go:build neo4j
// +build neo4j

package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// domainKnowledgeClientImpl provides domain knowledge operations over Neo4j
type domainKnowledgeClientImpl struct {
	driver neo4j.DriverWithContext
}

// Override the factory function when Neo4j is available
func NewDomainKnowledgeClient(uri, user, pass string) (DomainKnowledgeClient, error) {
	if strings.TrimSpace(uri) == "" {
		uri = "bolt://localhost:7687"
	}
	if strings.TrimSpace(user) == "" {
		user = "neo4j"
	}
	if strings.TrimSpace(pass) == "" {
		pass = "test1234"
	}

	drv, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, pass, ""))
	if err != nil {
		return nil, err
	}
	return &domainKnowledgeClientImpl{driver: drv}, nil
}

func (dk *domainKnowledgeClientImpl) Close(ctx context.Context) error {
	return dk.driver.Close(ctx)
}

// Domain Knowledge Types are defined in types.go

// Helper function to safely get string from map
func getStringFromMap(props map[string]any, key string) string {
	if val, exists := props[key]; exists && val != nil {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// Core Operations

// SaveConcept creates or updates a domain concept
func (dk *domainKnowledgeClientImpl) SaveConcept(ctx context.Context, concept *Concept) error {
	sess := dk.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer sess.Close(ctx)

	_, err := sess.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
		MERGE (c:Concept {name: $name, domain: $domain})
		SET c.definition = $definition,
		    c.created_at = COALESCE(c.created_at, datetime()),
		    c.updated_at = datetime()
		RETURN c`

		_, err := tx.Run(ctx, cypher, map[string]any{
			"name":       concept.Name,
			"domain":     concept.Domain,
			"definition": concept.Definition,
		})
		return nil, err
	})
	return err
}

// AddProperty adds a property to a concept
func (dk *domainKnowledgeClientImpl) AddProperty(ctx context.Context, conceptName, propertyName, description, propType string) error {
	sess := dk.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer sess.Close(ctx)

	_, err := sess.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
		MATCH (c:Concept {name: $conceptName})
		MERGE (p:Property {name: $propertyName})
		SET p.description = $description, p.type = $propType
		MERGE (c)-[:HAS_PROPERTY]->(p)
		RETURN c, p`

		_, err := tx.Run(ctx, cypher, map[string]any{
			"conceptName":  conceptName,
			"propertyName": propertyName,
			"description":  description,
			"propType":     propType,
		})
		return nil, err
	})
	return err
}

// AddConstraint adds a constraint to a concept
func (dk *domainKnowledgeClientImpl) AddConstraint(ctx context.Context, conceptName, description, constraintType, severity string) error {
	sess := dk.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer sess.Close(ctx)

	_, err := sess.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
		MATCH (c:Concept {name: $conceptName})
		CREATE (const:Constraint {
			description: $description,
			type: $constraintType,
			severity: $severity
		})
		MERGE (c)-[:HAS_CONSTRAINT]->(const)
		RETURN c, const`

		_, err := tx.Run(ctx, cypher, map[string]any{
			"conceptName":    conceptName,
			"description":    description,
			"constraintType": constraintType,
			"severity":       severity,
		})
		return nil, err
	})
	return err
}

// AddExample adds an example to a concept
func (dk *domainKnowledgeClientImpl) AddExample(ctx context.Context, conceptName string, example *Example) error {
	sess := dk.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer sess.Close(ctx)

	_, err := sess.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
		MATCH (c:Concept {name: $conceptName})
		CREATE (ex:Example {
			input: $input,
			output: $output,
			type: $exampleType
		})
		MERGE (c)-[:HAS_EXAMPLE]->(ex)
		RETURN c, ex`

		_, err := tx.Run(ctx, cypher, map[string]any{
			"conceptName": conceptName,
			"input":       example.Input,
			"output":      example.Output,
			"exampleType": example.Type,
		})
		return nil, err
	})
	return err
}

// RelateConcepts creates a relationship between two concepts
func (dk *domainKnowledgeClientImpl) RelateConcepts(ctx context.Context, srcConcept, relationType, dstConcept string, props map[string]any) error {
	sess := dk.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer sess.Close(ctx)

	_, err := sess.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
		MATCH (src:Concept {name: $srcConcept})
		MATCH (dst:Concept {name: $dstConcept})
		MERGE (src)-[r:RELATION {type: $relationType}]->(dst)
		SET r += $props
		RETURN src, r, dst`

		_, err := tx.Run(ctx, cypher, map[string]any{
			"srcConcept":   srcConcept,
			"dstConcept":   dstConcept,
			"relationType": relationType,
			"props":        props,
		})
		return nil, err
	})
	return err
}

// Query Operations

// GetConcept retrieves a concept with all its properties, constraints, and examples
func (dk *domainKnowledgeClientImpl) GetConcept(ctx context.Context, conceptName string) (*Concept, error) {
	sess := dk.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer sess.Close(ctx)

	result, err := sess.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
		MATCH (c:Concept {name: $conceptName})
		OPTIONAL MATCH (c)-[:HAS_PROPERTY]->(p:Property)
		OPTIONAL MATCH (c)-[:HAS_CONSTRAINT]->(const:Constraint)
		OPTIONAL MATCH (c)-[:HAS_EXAMPLE]->(ex:Example)
		RETURN c, collect(DISTINCT p) as properties, 
		       collect(DISTINCT const) as constraints,
		       collect(DISTINCT ex) as examples`

		records, err := tx.Run(ctx, cypher, map[string]any{"conceptName": conceptName})
		if err != nil {
			return nil, err
		}

		if records.Next(ctx) {
			record := records.Record()
			conceptNode, _ := record.Get("c")
			properties, _ := record.Get("properties")
			constraints, _ := record.Get("constraints")
			examples, _ := record.Get("examples")

			if c, ok := conceptNode.(neo4j.Node); ok {
				concept := &Concept{
					Name:       getStringFromMap(c.Props, "name"),
					Domain:     getStringFromMap(c.Props, "domain"),
					Definition: getStringFromMap(c.Props, "definition"),
				}

				// Parse properties
				if props, ok := properties.([]any); ok {
					for _, prop := range props {
						if p, ok := prop.(neo4j.Node); ok {
							concept.Properties = append(concept.Properties, getStringFromMap(p.Props, "name"))
						}
					}
				}

				// Parse constraints
				if consts, ok := constraints.([]any); ok {
					for _, constr := range consts {
						if c, ok := constr.(neo4j.Node); ok {
							concept.Constraints = append(concept.Constraints, getStringFromMap(c.Props, "description"))
						}
					}
				}

				// Parse examples
				if exs, ok := examples.([]any); ok {
					for _, ex := range exs {
						if e, ok := ex.(neo4j.Node); ok {
							concept.Examples = append(concept.Examples, Example{
								Input:  getStringFromMap(e.Props, "input"),
								Output: getStringFromMap(e.Props, "output"),
								Type:   getStringFromMap(e.Props, "type"),
							})
						}
					}
				}

				return concept, nil
			}
		}
		return nil, fmt.Errorf("concept not found: %s", conceptName)
	})

	if err != nil {
		return nil, err
	}
	return result.(*Concept), nil
}

// GetConstraints returns all constraints for a concept
func (dk *domainKnowledgeClientImpl) GetConstraints(ctx context.Context, conceptName string) ([]Constraint, error) {
	sess := dk.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer sess.Close(ctx)

	result, err := sess.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
		MATCH (c:Concept {name: $conceptName})-[:HAS_CONSTRAINT]->(const:Constraint)
		RETURN const`

		records, err := tx.Run(ctx, cypher, map[string]any{"conceptName": conceptName})
		if err != nil {
			return nil, err
		}

		var constraints []Constraint
		for records.Next(ctx) {
			record := records.Record()
			constNode, _ := record.Get("const")
			if c, ok := constNode.(neo4j.Node); ok {
				constraints = append(constraints, Constraint{
					Description: getStringFromMap(c.Props, "description"),
					Type:        getStringFromMap(c.Props, "type"),
					Severity:    getStringFromMap(c.Props, "severity"),
				})
			}
		}
		return constraints, records.Err()
	})

	if err != nil {
		return nil, err
	}
	return result.([]Constraint), nil
}

// GetProperties returns all properties for a concept
func (dk *domainKnowledgeClientImpl) GetProperties(ctx context.Context, conceptName string) ([]Property, error) {
	sess := dk.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer sess.Close(ctx)

	result, err := sess.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
		MATCH (c:Concept {name: $conceptName})-[:HAS_PROPERTY]->(p:Property)
		RETURN p`

		records, err := tx.Run(ctx, cypher, map[string]any{"conceptName": conceptName})
		if err != nil {
			return nil, err
		}

		var properties []Property
		for records.Next(ctx) {
			record := records.Record()
			propNode, _ := record.Get("p")
			if p, ok := propNode.(neo4j.Node); ok {
				properties = append(properties, Property{
					Name:        getStringFromMap(p.Props, "name"),
					Description: getStringFromMap(p.Props, "description"),
					Type:        getStringFromMap(p.Props, "type"),
				})
			}
		}
		return properties, records.Err()
	})

	if err != nil {
		return nil, err
	}
	return result.([]Property), nil
}

// GetExamples returns all examples for a concept
func (dk *domainKnowledgeClientImpl) GetExamples(ctx context.Context, conceptName string) ([]Example, error) {
	sess := dk.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer sess.Close(ctx)

	result, err := sess.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
		MATCH (c:Concept {name: $conceptName})-[:HAS_EXAMPLE]->(ex:Example)
		RETURN ex`

		records, err := tx.Run(ctx, cypher, map[string]any{"conceptName": conceptName})
		if err != nil {
			return nil, err
		}

		var examples []Example
		for records.Next(ctx) {
			record := records.Record()
			exNode, _ := record.Get("ex")
			if e, ok := exNode.(neo4j.Node); ok {
				examples = append(examples, Example{
					Input:  getStringFromMap(e.Props, "input"),
					Output: getStringFromMap(e.Props, "output"),
					Type:   getStringFromMap(e.Props, "type"),
				})
			}
		}
		return examples, records.Err()
	})

	if err != nil {
		return nil, err
	}
	return result.([]Example), nil
}

// SearchConcepts finds concepts by domain or name pattern
func (dk *domainKnowledgeClientImpl) SearchConcepts(ctx context.Context, domain, namePattern string, limit int) ([]Concept, error) {
	sess := dk.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer sess.Close(ctx)

	if limit <= 0 {
		limit = 50
	}

	result, err := sess.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		var cypher string
		var params map[string]any

		if domain != "" && namePattern != "" {
			cypher = `
			MATCH (c:Concept)
			WHERE c.domain = $domain AND c.name CONTAINS $namePattern
			RETURN c
			LIMIT $limit`
			params = map[string]any{"domain": domain, "namePattern": namePattern, "limit": limit}
		} else if domain != "" {
			cypher = `
			MATCH (c:Concept)
			WHERE c.domain = $domain
			RETURN c
			LIMIT $limit`
			params = map[string]any{"domain": domain, "limit": limit}
		} else if namePattern != "" {
			cypher = `
			MATCH (c:Concept)
			WHERE c.name CONTAINS $namePattern
			RETURN c
			LIMIT $limit`
			params = map[string]any{"namePattern": namePattern, "limit": limit}
		} else {
			cypher = `
			MATCH (c:Concept)
			RETURN c
			LIMIT $limit`
			params = map[string]any{"limit": limit}
		}

		records, err := tx.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}

		var concepts []Concept
		for records.Next(ctx) {
			record := records.Record()
			conceptNode, _ := record.Get("c")
			if c, ok := conceptNode.(neo4j.Node); ok {
				concept := Concept{
					Name:       getStringFromMap(c.Props, "name"),
					Domain:     getStringFromMap(c.Props, "domain"),
					Definition: getStringFromMap(c.Props, "definition"),
				}
				concepts = append(concepts, concept)
			}
		}
		return concepts, records.Err()
	})

	if err != nil {
		return nil, err
	}
	return result.([]Concept), nil
}

// GetRelatedConcepts finds concepts related to a given concept
func (dk *domainKnowledgeClientImpl) GetRelatedConcepts(ctx context.Context, conceptName string, relationTypes []string) ([]Concept, error) {
	sess := dk.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer sess.Close(ctx)

	result, err := sess.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		var cypher string
		var params map[string]any

		if len(relationTypes) > 0 {
			cypher = `
			MATCH (c:Concept {name: $conceptName})-[r:RELATION]->(related:Concept)
			WHERE r.type IN $relationTypes
			RETURN related`
			params = map[string]any{"conceptName": conceptName, "relationTypes": relationTypes}
		} else {
			cypher = `
			MATCH (c:Concept {name: $conceptName})-[r:RELATION]->(related:Concept)
			RETURN related`
			params = map[string]any{"conceptName": conceptName}
		}

		records, err := tx.Run(ctx, cypher, params)
		if err != nil {
			return nil, err
		}

		var concepts []Concept
		for records.Next(ctx) {
			record := records.Record()
			conceptNode, _ := record.Get("related")
			if c, ok := conceptNode.(neo4j.Node); ok {
				concepts = append(concepts, Concept{
					Name:       getStringFromMap(c.Props, "name"),
					Domain:     getStringFromMap(c.Props, "domain"),
					Definition: getStringFromMap(c.Props, "definition"),
				})
			}
		}
		return concepts, records.Err()
	})

	if err != nil {
		return nil, err
	}
	return result.([]Concept), nil
}

// LinkToPrinciple links a concept to a safety principle
func (dk *domainKnowledgeClientImpl) LinkToPrinciple(ctx context.Context, conceptName, principleName, principleDescription string) error {
	sess := dk.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer sess.Close(ctx)

	_, err := sess.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		cypher := `
		MATCH (c:Concept {name: $conceptName})
		MERGE (p:Principle {name: $principleName})
		SET p.description = $principleDescription
		MERGE (c)-[:BLOCKED_BY]->(p)
		RETURN c, p`

		_, err := tx.Run(ctx, cypher, map[string]any{
			"conceptName":          conceptName,
			"principleName":        principleName,
			"principleDescription": principleDescription,
		})
		return nil, err
	})
	return err
}
