//go:build neo4j
// +build neo4j

package main

import (
	"context"
	"fmt"
	"log"

	mempkg "agi/hdn/memory"
)

// Example of how to integrate domain knowledge into planner/evaluator decision making
func main() {
	// Initialize domain knowledge client
	client, err := mempkg.NewDomainKnowledgeClient("bolt://localhost:7687", "neo4j", "test1234")
	if err != nil {
		log.Fatalf("Failed to connect to Neo4j: %v", err)
	}
	defer client.Close(context.Background())

	ctx := context.Background()

	fmt.Println("🧠 Domain Knowledge Integration Example")
	fmt.Println("=====================================")

	// Example 1: Validate matrix multiplication inputs
	fmt.Println("\n📐 Example 1: Matrix Multiplication Validation")
	validateMatrixMultiplication(client, ctx, [][]int{{1, 2}, {3, 4}}, [][]int{{5, 6}, {7, 8}})
	validateMatrixMultiplication(client, ctx, [][]int{{1, 2}, {3, 4}}, [][]int{{5, 6, 7}, {8, 9, 10}}) // Invalid

	// Example 2: Check safety principles
	fmt.Println("\n🛡️ Example 2: Safety Principle Checking")
	checkSafetyPrinciples(client, ctx, "Docker Container")
	checkSafetyPrinciples(client, ctx, "Redis Cache")

	// Example 3: Find related concepts for planning
	fmt.Println("\n🔗 Example 3: Finding Related Concepts")
	findRelatedConcepts(client, ctx, "Matrix Multiplication")

	// Example 4: Search for domain-specific concepts
	fmt.Println("\n🔍 Example 4: Domain-Specific Search")
	searchDomainConcepts(client, ctx, "Math")
	searchDomainConcepts(client, ctx, "Programming")
}

func validateMatrixMultiplication(client mempkg.DomainKnowledgeClient, ctx context.Context, matrixA, matrixB [][]int) {
	fmt.Printf("Validating: %v × %v\n", matrixA, matrixB)

	// Get constraints for matrix multiplication
	constraints, err := client.GetConstraints(ctx, "Matrix Multiplication")
	if err != nil {
		fmt.Printf("❌ Error getting constraints: %v\n", err)
		return
	}

	// Check dimension constraint
	valid := true
	for _, constraint := range constraints {
		if constraint.Type == "dimension" {
			// Check if A.cols == B.rows
			if len(matrixA[0]) != len(matrixB) {
				fmt.Printf("❌ Constraint violated: %s\n", constraint.Description)
				valid = false
			}
		}
	}

	if valid {
		fmt.Println("✅ Matrix multiplication is valid")
	} else {
		fmt.Println("❌ Matrix multiplication is invalid")
	}
}

func checkSafetyPrinciples(client mempkg.DomainKnowledgeClient, ctx context.Context, conceptName string) {
	fmt.Printf("Checking safety principles for: %s\n", conceptName)

	// Get the concept
	concept, err := client.GetConcept(ctx, conceptName)
	if err != nil {
		fmt.Printf("❌ Error getting concept: %v\n", err)
		return
	}

	// In a real implementation, you would query for principles linked to this concept
	// For now, we'll just show the concept properties
	fmt.Printf("✅ Concept found: %s\n", concept.Name)
	fmt.Printf("   Domain: %s\n", concept.Domain)
	fmt.Printf("   Definition: %s\n", concept.Definition)

	if len(concept.Properties) > 0 {
		fmt.Printf("   Properties: %v\n", concept.Properties)
	}
	if len(concept.Constraints) > 0 {
		fmt.Printf("   Constraints: %v\n", concept.Constraints)
	}
}

func findRelatedConcepts(client mempkg.DomainKnowledgeClient, ctx context.Context, conceptName string) {
	fmt.Printf("Finding concepts related to: %s\n", conceptName)

	// Get related concepts
	related, err := client.GetRelatedConcepts(ctx, conceptName, nil)
	if err != nil {
		fmt.Printf("❌ Error getting related concepts: %v\n", err)
		return
	}

	if len(related) == 0 {
		fmt.Println("   No related concepts found")
		return
	}

	fmt.Printf("   Found %d related concepts:\n", len(related))
	for _, concept := range related {
		fmt.Printf("   - %s (%s): %s\n", concept.Name, concept.Domain, concept.Definition)
	}
}

func searchDomainConcepts(client mempkg.DomainKnowledgeClient, ctx context.Context, domain string) {
	fmt.Printf("Searching concepts in domain: %s\n", domain)

	// Search concepts by domain
	concepts, err := client.SearchConcepts(ctx, domain, "", 10)
	if err != nil {
		fmt.Printf("❌ Error searching concepts: %v\n", err)
		return
	}

	if len(concepts) == 0 {
		fmt.Println("   No concepts found in this domain")
		return
	}

	fmt.Printf("   Found %d concepts in %s domain:\n", len(concepts), domain)
	for _, concept := range concepts {
		fmt.Printf("   - %s: %s\n", concept.Name, concept.Definition)
	}
}
