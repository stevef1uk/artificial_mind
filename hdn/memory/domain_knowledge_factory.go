//go:build !neo4j
// +build !neo4j

package memory

import (
	"fmt"
)

// NewDomainKnowledgeClient creates a new domain knowledge client
// Returns nil if Neo4j is not available or build tags are not enabled
// This function is overridden in domain_knowledge.go when Neo4j is available
func NewDomainKnowledgeClient(uri, user, pass string) (DomainKnowledgeClient, error) {
	// This function will be implemented in domain_knowledge.go with build tags
	// For now, return nil to indicate Neo4j is not available
	return nil, fmt.Errorf("domain knowledge requires Neo4j build tag")
}

// newDomainKnowledgeClientImpl is implemented in domain_knowledge.go with build tags
