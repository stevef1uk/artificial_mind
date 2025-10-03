package main

import (
	"fmt"

	"github.com/nats-io/nats.go"
)

func main() {
	fmt.Printf("nats.DefaultURL: %s\n", nats.DefaultURL)
}
