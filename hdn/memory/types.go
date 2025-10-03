package memory

import (
	"context"
	"time"
)

// Domain Knowledge Types

type Concept struct {
	Name        string    `json:"name"`
	Domain      string    `json:"domain"`
	Definition  string    `json:"definition"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Properties  []string  `json:"properties,omitempty"`
	Constraints []string  `json:"constraints,omitempty"`
	Examples    []Example `json:"examples,omitempty"`
}

type Example struct {
	Input  string `json:"input"`
	Output string `json:"output"`
	Type   string `json:"type,omitempty"`
}

type Constraint struct {
	Description string `json:"description"`
	Type        string `json:"type"`     // dimension, safety, logical, etc.
	Severity    string `json:"severity"` // error, warning, info
}

type Property struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"` // algebraic, physical, logical, etc.
}

type Principle struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category,omitempty"`
}

// DomainKnowledgeClient interface for domain knowledge operations
type DomainKnowledgeClient interface {
	Close(ctx context.Context) error
	SaveConcept(ctx context.Context, concept *Concept) error
	AddProperty(ctx context.Context, conceptName, propertyName, description, propType string) error
	AddConstraint(ctx context.Context, conceptName, description, constraintType, severity string) error
	AddExample(ctx context.Context, conceptName string, example *Example) error
	RelateConcepts(ctx context.Context, srcConcept, relationType, dstConcept string, props map[string]any) error
	GetConcept(ctx context.Context, conceptName string) (*Concept, error)
	GetConstraints(ctx context.Context, conceptName string) ([]Constraint, error)
	GetProperties(ctx context.Context, conceptName string) ([]Property, error)
	GetExamples(ctx context.Context, conceptName string) ([]Example, error)
	SearchConcepts(ctx context.Context, domain, namePattern string, limit int) ([]Concept, error)
	GetRelatedConcepts(ctx context.Context, conceptName string, relationTypes []string) ([]Concept, error)
	LinkToPrinciple(ctx context.Context, conceptName, principleName, principleDescription string) error
}
