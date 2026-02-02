module fsm

go 1.24.4

require (
	agi/hdn v0.0.0-00010101000000-000000000000
	agi/self v0.0.0-00010101000000-000000000000
	github.com/joho/godotenv v1.5.1
	github.com/nats-io/nats.go v1.46.0
	github.com/redis/go-redis/v9 v9.14.0
	gopkg.in/yaml.v2 v2.4.0
)

replace agi/hdn => ../hdn

replace agi/self => ../self

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/nats-io/nkeys v0.4.11 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/neo4j/neo4j-go-driver/v5 v5.25.0 // indirect
	golang.org/x/crypto v0.45.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
)
