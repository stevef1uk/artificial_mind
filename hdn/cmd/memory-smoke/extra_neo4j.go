//go:build neo4j
// +build neo4j

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	mem "hdn/memory"
)

func runExtra() {
	uri := getenv("NEO4J_URI", "bolt://localhost:7687")
	user := getenv("NEO4J_USER", "neo4j")
	pass := getenv("NEO4J_PASS", "test1234")

	kb, err := mem.NewSemanticKB(uri, user, pass)
	if err != nil {
		fmt.Println("❌ neo4j connect failed:", err)
		os.Exit(1)
	}
	defer kb.Close(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Upsert an entity and read it back
	if err := kb.UpsertEntity(ctx, "Concept", "smoke_demo_entity", map[string]interface{}{"name": "SmokeDemo", "kind": "test"}); err != nil {
		fmt.Println("❌ neo4j upsert failed:", err)
		os.Exit(1)
	}

	props, err := kb.GetEntity(ctx, "Concept", "smoke_demo_entity")
	if err != nil {
		fmt.Println("❌ neo4j get failed:", err)
		os.Exit(1)
	}
	if props["name"] != "SmokeDemo" {
		fmt.Println("❌ neo4j entity mismatch")
		os.Exit(1)
	}
	fmt.Println("✅ neo4j CRUD ok")
}
