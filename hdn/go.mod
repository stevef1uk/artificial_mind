module hdn

go 1.23.0

require (
	agi/planner_evaluator v0.0.0-00010101000000-000000000000
	agi/self v0.0.0-00010101000000-000000000000
	eventbus v0.0.0
	github.com/alicebob/miniredis/v2 v2.35.0
	github.com/gorilla/mux v1.8.0
	github.com/nats-io/nats.go v1.46.0
	github.com/neo4j/neo4j-go-driver/v5 v5.25.0
	github.com/redis/go-redis/v9 v9.14.0
	principles v0.0.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/nats-io/nkeys v0.4.11 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/sys v0.32.0 // indirect
)

replace principles => ../principles

replace agi/self => ../self

replace agi/planner_evaluator => ../planner_evaluator

replace eventbus => ../eventbus
