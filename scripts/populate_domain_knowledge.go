//go:build neo4j
// +build neo4j

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	mempkg "agi/hdn/memory"
)

func main() {
	// Get Neo4j connection details from environment
	neo4jURI := os.Getenv("NEO4J_URI")
	if neo4jURI == "" {
		neo4jURI = "bolt://localhost:7687"
	}
	neo4jUser := os.Getenv("NEO4J_USER")
	if neo4jUser == "" {
		neo4jUser = "neo4j"
	}
	neo4jPass := os.Getenv("NEO4J_PASS")
	if neo4jPass == "" {
		neo4jPass = "test1234"
	}

	// Initialize domain knowledge client
	client, err := mempkg.NewDomainKnowledgeClient(neo4jURI, neo4jUser, neo4jPass)
	if err != nil {
		log.Fatalf("Failed to connect to Neo4j: %v", err)
	}
	defer client.Close(context.Background())

	ctx := context.Background()

	fmt.Println("🧠 Populating domain knowledge...")

	// 1. Mathematical Concepts
	fmt.Println("📐 Adding mathematical concepts...")

	// Matrix Multiplication
	matrixMult := &mempkg.Concept{
		Name:       "Matrix Multiplication",
		Domain:     "Math",
		Definition: "The operation of multiplying two matrices A and B to produce matrix C",
	}
	if err := client.SaveConcept(ctx, matrixMult); err != nil {
		log.Printf("Error saving Matrix Multiplication: %v", err)
	}

	// Add constraints
	client.AddConstraint(ctx, "Matrix Multiplication", "Number of columns of A must equal number of rows of B", "dimension", "error")
	client.AddConstraint(ctx, "Matrix Multiplication", "Result matrix has dimensions A.rows × B.cols", "dimension", "info")

	// Add properties
	client.AddProperty(ctx, "Matrix Multiplication", "Associative", "Matrix multiplication is associative: (AB)C = A(BC)", "algebraic")
	client.AddProperty(ctx, "Matrix Multiplication", "Distributive", "Matrix multiplication is distributive: A(B+C) = AB + AC", "algebraic")
	client.AddProperty(ctx, "Matrix Multiplication", "Not Commutative", "Matrix multiplication is generally not commutative: AB ≠ BA", "algebraic")

	// Add examples
	client.AddExample(ctx, "Matrix Multiplication", &mempkg.Example{
		Input:  "[[1,2],[3,4]] × [[5,6],[7,8]]",
		Output: "[[19,22],[43,50]]",
		Type:   "numeric",
	})

	// Prime Numbers
	primeNumbers := &mempkg.Concept{
		Name:       "Prime Number",
		Domain:     "Math",
		Definition: "A natural number greater than 1 that has no positive divisors other than 1 and itself",
	}
	if err := client.SaveConcept(ctx, primeNumbers); err != nil {
		log.Printf("Error saving Prime Number: %v", err)
	}

	client.AddConstraint(ctx, "Prime Number", "Must be greater than 1", "logical", "error")
	client.AddConstraint(ctx, "Prime Number", "Must have exactly 2 divisors", "logical", "error")

	client.AddProperty(ctx, "Prime Number", "Unique Factorization", "Every integer > 1 can be written as a product of primes", "algebraic")
	client.AddProperty(ctx, "Prime Number", "Infinite", "There are infinitely many prime numbers", "logical")

	client.AddExample(ctx, "Prime Number", &mempkg.Example{
		Input:  "2, 3, 5, 7, 11, 13, 17, 19, 23, 29",
		Output: "First 10 prime numbers",
		Type:   "sequence",
	})

	// 2. Programming Concepts
	fmt.Println("💻 Adding programming concepts...")

	// Docker Container
	dockerContainer := &mempkg.Concept{
		Name:       "Docker Container",
		Domain:     "Programming",
		Definition: "A lightweight, portable unit that packages an application and its dependencies",
	}
	if err := client.SaveConcept(ctx, dockerContainer); err != nil {
		log.Printf("Error saving Docker Container: %v", err)
	}

	client.AddConstraint(ctx, "Docker Container", "Must have a base image", "structural", "error")
	client.AddConstraint(ctx, "Docker Container", "Must define entrypoint or CMD", "structural", "warning")

	client.AddProperty(ctx, "Docker Container", "Isolation", "Processes run in isolated environment", "system")
	client.AddProperty(ctx, "Docker Container", "Portability", "Can run on any system with Docker", "system")
	client.AddProperty(ctx, "Docker Container", "Stateless", "Should not store persistent data in container", "design")

	client.AddExample(ctx, "Docker Container", &mempkg.Example{
		Input:  "FROM python:3.9\nRUN pip install requests\nCOPY app.py .\nCMD [\"python\", \"app.py\"]",
		Output: "Basic Python Dockerfile",
		Type:   "code",
	})

	// 3. System Concepts
	fmt.Println("🔧 Adding system concepts...")

	// Redis Cache
	redisCache := &mempkg.Concept{
		Name:       "Redis Cache",
		Domain:     "System",
		Definition: "In-memory data structure store used as a database, cache, and message broker",
	}
	if err := client.SaveConcept(ctx, redisCache); err != nil {
		log.Printf("Error saving Redis Cache: %v", err)
	}

	client.AddConstraint(ctx, "Redis Cache", "Data is stored in memory", "performance", "info")
	client.AddConstraint(ctx, "Redis Cache", "Data can be lost on restart", "persistence", "warning")

	client.AddProperty(ctx, "Redis Cache", "Fast Access", "Sub-millisecond response times", "performance")
	client.AddProperty(ctx, "Redis Cache", "Key-Value Store", "Simple key-value data model", "structural")
	client.AddProperty(ctx, "Redis Cache", "TTL Support", "Time-to-live for automatic expiration", "feature")

	// 4. Create Relationships
	fmt.Println("🔗 Creating relationships...")

	// Matrix Multiplication requires Matrix
	matrix := &mempkg.Concept{
		Name:       "Matrix",
		Domain:     "Math",
		Definition: "A rectangular array of numbers, symbols, or expressions arranged in rows and columns",
	}
	client.SaveConcept(ctx, matrix)

	client.RelateConcepts(ctx, "Matrix Multiplication", "REQUIRES", "Matrix", map[string]interface{}{
		"description": "Matrix multiplication requires matrix operands",
	})

	// Docker Container uses Redis Cache
	client.RelateConcepts(ctx, "Docker Container", "CAN_USE", "Redis Cache", map[string]interface{}{
		"description": "Docker containers can use Redis for caching",
	})

	// Prime Numbers are used in Matrix operations
	client.RelateConcepts(ctx, "Prime Number", "USED_IN", "Matrix Multiplication", map[string]interface{}{
		"description": "Prime numbers can be used in matrix operations for encryption",
	})

	// 5. Safety Principles
	fmt.Println("🛡️ Adding safety principles...")

	// Link concepts to safety principles
	client.LinkToPrinciple(ctx, "Docker Container", "SafeExecution", "Code execution must be sandboxed and isolated")
	client.LinkToPrinciple(ctx, "Redis Cache", "DataProtection", "Sensitive data should not be cached without encryption")
	client.LinkToPrinciple(ctx, "Matrix Multiplication", "SafeMath", "Mathematical operations must not cause overflow or underflow")

	fmt.Println("✅ Domain knowledge population complete!")
	fmt.Println("\n📊 Summary:")
	fmt.Println("- Mathematical concepts: Matrix Multiplication, Prime Numbers")
	fmt.Println("- Programming concepts: Docker Container")
	fmt.Println("- System concepts: Redis Cache")
	fmt.Println("- Relationships: 3 concept relationships created")
	fmt.Println("- Safety principles: 3 safety links created")
}
