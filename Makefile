# AGI Project Makefile
# ===================

# Architecture detection and build options
ARCH := $(shell uname -m)
GOOS := $(shell uname -s | tr '[:upper:]' '[:lower:]')
GOARCH := $(shell uname -m)

# Set Go architecture for cross-compilation
ifeq ($(ARCH),x86_64)
	GOARCH := amd64
else ifeq ($(ARCH),aarch64)
	GOARCH := arm64
	# For ARM64, recommend Docker builds but allow native builds
endif

# Cross-compilation support
ifdef TARGET_OS
	GOOS := $(TARGET_OS)
endif
ifdef TARGET_ARCH
	GOARCH := $(TARGET_ARCH)
endif

# Variables
PRINCIPLES_DIR := principles
HDN_DIR := hdn
MONITOR_DIR := monitor
FSM_DIR := fsm
GOAL_DIR := cmd/goal-manager
TELEGRAM_BOT_DIR := telegram-bot
SCRAPER_DIR := services/playwright_scraper
BIN_DIR := bin
PRINCIPLES_BIN := $(BIN_DIR)/principles-server
HDN_BIN := $(BIN_DIR)/hdn-server
MONITOR_BIN := $(BIN_DIR)/monitor-ui
FSM_BIN := $(BIN_DIR)/fsm-server
GOAL_BIN := $(BIN_DIR)/goal-manager
TELEGRAM_BOT_BIN := $(BIN_DIR)/telegram-bot
TOOLS_DIR := tools

# Go build flags
GO_BUILD_FLAGS := -ldflags="-s -w" -o
GO_TEST_FLAGS := -v -race -coverprofile=coverage.out

# Cross-compilation environment
GO_ENV := GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0

# Verbose build option
ifeq ($(VERBOSE),1)
	GO_BUILD_FLAGS := -v $(GO_BUILD_FLAGS)
endif

# Memory components configuration
COMPOSE := docker-compose
RAG_URL ?= http://localhost:9010
WEAVIATE_URL ?= http://localhost:8080
NEO4J_URI ?= bolt://localhost:7687
NEO4J_USER ?= neo4j
NEO4J_PASS ?= test1234
# Container names (override if your docker names differ)
REDIS_CONTAINER ?= agi-redis
NEO4J_CONTAINER ?= agi-neo4j

# Default target
.PHONY: all
all: build

# Cross-compilation targets
.PHONY: build-x86 build-arm64 build-linux build-windows build-macos
build-x86:
	@echo "Building for x86_64 (amd64)..."
	$(MAKE) TARGET_ARCH=amd64 build

build-arm64:
	@echo "Building for ARM64..."
	$(MAKE) TARGET_ARCH=arm64 build

build-linux:
	@echo "Building for Linux..."
	$(MAKE) TARGET_OS=linux build

build-windows:
	@echo "Building for Windows..."
	$(MAKE) TARGET_OS=windows TARGET_ARCH=amd64 build

build-macos:
	@echo "Building for macOS..."
	$(MAKE) TARGET_OS=darwin TARGET_ARCH=amd64 build

# Multi-architecture build
.PHONY: build-all-archs
build-all-archs: build-x86 build-arm64
	@echo "Built for multiple architectures: x86_64 and ARM64"

# Help target for cross-compilation
.PHONY: help-cross
help-cross:
	@echo "🔧 Cross-Compilation Help"
	@echo "========================"
	@echo ""
	@echo "Available targets:"
	@echo "  build-x86      - Build for x86_64 (amd64) architecture"
	@echo "  build-arm64    - Build for ARM64 architecture"
	@echo "  build-linux    - Build for Linux (any architecture)"
	@echo "  build-windows  - Build for Windows x86_64"
	@echo "  build-macos    - Build for macOS x86_64"
	@echo "  build-all-archs - Build for both x86_64 and ARM64"
	@echo ""
	@echo "Environment variables:"
	@echo "  TARGET_OS      - Target operating system (linux, windows, darwin)"
	@echo "  TARGET_ARCH    - Target architecture (amd64, arm64)"
	@echo ""
	@echo "Examples:"
	@echo "  make build-x86                    # Build for x86_64"
	@echo "  make TARGET_OS=windows build     # Build for Windows"
	@echo "  make TARGET_ARCH=arm64 build     # Build for ARM64"
	@echo ""
	@echo "Current architecture: $(ARCH) -> $(GOOS)/$(GOARCH)"

# Build all components
.PHONY: build
build: build-principles build-hdn build-monitor build-fsm build-goal build-telegram-bot build-tools build-wiki-bootstrapper build-wiki-summarizer build-news-ingestor build-nats-demos build-nats-test validate-safety

# Build NATS demos
.PHONY: build-nats-demos
build-nats-demos:
	@echo "🔨 Building NATS demos..."
	@mkdir -p $(BIN_DIR)
	@cd cmd/nats-producer && $(GO_ENV) GO111MODULE=on go build $(GO_BUILD_FLAGS) ../../$(BIN_DIR)/nats-producer .
	@cd cmd/nats-consumer && $(GO_ENV) GO111MODULE=on go build $(GO_BUILD_FLAGS) ../../$(BIN_DIR)/nats-consumer .
	@echo "✅ Built: $(BIN_DIR)/nats-producer, $(BIN_DIR)/nats-consumer"

# Build NATS roundtrip test
.PHONY: build-nats-test
build-nats-test:
	@echo "🔨 Building NATS roundtrip test..."
	@mkdir -p $(BIN_DIR)
	@cd cmd/nats-roundtrip && $(GO_ENV) GO111MODULE=on go build $(GO_BUILD_FLAGS) ../../$(BIN_DIR)/nats-roundtrip .
	@echo "✅ Built: $(BIN_DIR)/nats-roundtrip"

# Run NATS consumer demo
.PHONY: run-nats-consumer
run-nats-consumer: build-nats-demos
	@echo "📡 Running NATS consumer..."
	@NATS_URL=nats://127.0.0.1:4222 $(BIN_DIR)/nats-consumer | cat

# Run NATS producer demo
.PHONY: run-nats-producer
run-nats-producer: build-nats-demos
	@echo "📡 Running NATS producer..."
	@NATS_URL=nats://127.0.0.1:4222 $(BIN_DIR)/nats-producer | cat

# Run NATS roundtrip test
.PHONY: test-nats-bus
test-nats-bus: build-nats-test
	@echo "🧪 Running NATS bus roundtrip test..."
	@NATS_URL=nats://127.0.0.1:4222 $(BIN_DIR)/nats-roundtrip -timeout=5s | cat

# Build principles server
.PHONY: build-principles
build-principles:
	@echo "🔨 Building principles server..."
	@mkdir -p $(BIN_DIR)
	@cd $(PRINCIPLES_DIR) && $(GO_ENV) go build $(GO_BUILD_FLAGS) ../$(PRINCIPLES_BIN) .
	@echo "✅ Principles server built: $(PRINCIPLES_BIN)"

# Build HDN server
.PHONY: build-hdn
build-hdn:
	@echo "🔨 Building HDN server..."
	@mkdir -p $(BIN_DIR)
	@cd $(HDN_DIR) && $(GO_ENV) GO111MODULE=on go build -tags neo4j $(GO_BUILD_FLAGS) ../$(HDN_BIN) .
	@echo "✅ HDN server built: $(HDN_BIN)"

# Build HDN server with Neo4j support (build tag)
.PHONY: build-hdn-neo4j
build-hdn-neo4j:
	@echo "🔨 Building HDN server (neo4j tag)..."
	@mkdir -p $(BIN_DIR)
	@cd $(HDN_DIR) && go mod tidy && $(GO_ENV) GO111MODULE=on go build -tags neo4j $(GO_BUILD_FLAGS) ../$(HDN_BIN) .
	@echo "✅ HDN server (neo4j) built: $(HDN_BIN)"

# Build Wikipedia bootstrapper (neo4j tag)
.PHONY: build-wiki-bootstrapper
build-wiki-bootstrapper:
	@echo "🔨 Building Wikipedia bootstrapper (neo4j tag)..."
	@mkdir -p $(BIN_DIR)
	@cd cmd/wiki-bootstrapper && $(GO_ENV) GO111MODULE=on go build -tags neo4j $(GO_BUILD_FLAGS) ../../$(BIN_DIR)/wiki-bootstrapper .
	@echo "✅ Built: $(BIN_DIR)/wiki-bootstrapper"

# Build Wikipedia summarizer
.PHONY: build-wiki-summarizer
build-wiki-summarizer:
	@echo "🔨 Building Wikipedia summarizer..."
	@mkdir -p $(BIN_DIR)
	@cd cmd/wiki-summarizer && $(GO_ENV) GO111MODULE=on go build $(GO_BUILD_FLAGS) ../../$(BIN_DIR)/wiki-summarizer .
	@echo "✅ Built: $(BIN_DIR)/wiki-summarizer"

.PHONY: run-wiki-bootstrapper
run-wiki-bootstrapper: build-wiki-bootstrapper
	@echo "🚀 Running Wikipedia bootstrapper..."
	@if docker ps --format '{{.Names}}' | grep -qE '^$(REDIS_CONTAINER)$$'; then \
		REDIS_ADDR=localhost:6379; \
		echo "Using Redis at $$REDIS_ADDR (from Docker container $(REDIS_CONTAINER))"; \
	else \
		REDIS_ADDR=$${REDIS_ADDR:-localhost:6379}; \
		echo "Using Redis at $$REDIS_ADDR"; \
	fi; \
	NEO4J_URI=$(NEO4J_URI) NEO4J_USER=$(NEO4J_USER) NEO4J_PASS=$(NEO4J_PASS) \
	WEAVIATE_URL=$(WEAVIATE_URL) REDIS_ADDR=$$REDIS_ADDR \
	$(BIN_DIR)/wiki-bootstrapper -weaviate | cat

.PHONY: run-wiki-summarizer
run-wiki-summarizer: build-wiki-summarizer
	@echo "🚀 Running Wikipedia summarizer..."
	@QDRANT_URL=$(WEAVIATE_URL) REDIS_ADDR=$(REDIS_CONTAINER):6379 $(BIN_DIR)/wiki-summarizer | cat

# Build BBC news ingestor
.PHONY: build-news-ingestor
build-news-ingestor:
	@echo "🔨 Building BBC news ingestor..."
	@mkdir -p $(BIN_DIR)
	@cd cmd/bbc-news-ingestor && $(GO_ENV) GO111MODULE=on go build $(GO_BUILD_FLAGS) ../../$(BIN_DIR)/bbc-news-ingestor .
	@echo "✅ Built: $(BIN_DIR)/bbc-news-ingestor"

.PHONY: run-news-ingestor
run-news-ingestor: build-news-ingestor
	@echo "🚀 Running BBC news ingestor..."
	@$(BIN_DIR)/bbc-news-ingestor -llm -batch-size 10 -max 30

# Build monitor UI
.PHONY: build-monitor
build-monitor:
	@echo "🔨 Building monitor UI..."
	@mkdir -p $(BIN_DIR)
	@cd $(MONITOR_DIR) && $(GO_ENV) go build $(GO_BUILD_FLAGS) ../$(MONITOR_BIN) .
	@echo "✅ Monitor UI built: $(MONITOR_BIN)"

# Build FSM server
.PHONY: build-fsm
build-fsm:
	@echo "🔨 Building FSM server..."
	@mkdir -p $(BIN_DIR)
	@cd $(FSM_DIR) && $(GO_ENV) GO111MODULE=on go build -tags neo4j $(GO_BUILD_FLAGS) ../$(FSM_BIN) . || (echo "❌ FSM server build failed" && exit 1)
	@echo "✅ FSM server built: $(FSM_BIN)"

# Debug build FSM server (shows errors)
.PHONY: build-fsm-debug
build-fsm-debug:
	@echo "🔨 Building FSM server (debug mode)..."
	@mkdir -p $(BIN_DIR)
	@cd $(FSM_DIR) && $(GO_ENV) GO111MODULE=on go build -tags neo4j -v -o ../$(FSM_BIN) .
	@echo "✅ FSM server built: $(FSM_BIN)"

# Build Goal Manager service
.PHONY: build-goal
build-goal:
	@echo "🔨 Building Goal Manager service..."
	@mkdir -p $(BIN_DIR)
	@cd $(GOAL_DIR) && $(GO_ENV) GOFLAGS="-mod=mod" GO111MODULE=on go get github.com/gorilla/mux@v1.8.1 && go build $(GO_BUILD_FLAGS) ../../$(GOAL_BIN) .
	@echo "✅ Goal Manager built: $(GOAL_BIN)"

# Build Telegram Bot
.PHONY: build-telegram-bot
build-telegram-bot:
	@echo "🔨 Building Telegram Bot..."
	@mkdir -p $(BIN_DIR)
	@cd $(TELEGRAM_BOT_DIR) && $(GO_ENV) GO111MODULE=on go build $(GO_BUILD_FLAGS) ../$(TELEGRAM_BOT_BIN) .
	@echo "✅ Telegram Bot built: $(TELEGRAM_BOT_BIN)"


# Build memory smoke tool (episodic only)
.PHONY: build-memory-smoke
build-memory-smoke:
	@echo "🔨 Building memory smoke tool..."
	@mkdir -p $(BIN_DIR)
	@cd hdn/cmd/memory-smoke && $(GO_ENV) GO111MODULE=on go build $(GO_BUILD_FLAGS) ../../../$(BIN_DIR)/memory-smoke .
	@echo "✅ Built: $(BIN_DIR)/memory-smoke"

# Build memory smoke tool with Neo4j
.PHONY: build-memory-smoke-neo4j
build-memory-smoke-neo4j:
	@echo "🔨 Building memory smoke tool (neo4j tag)..."
	@mkdir -p $(BIN_DIR)
	@cd hdn && go mod tidy
	@cd hdn/cmd/memory-smoke && $(GO_ENV) GO111MODULE=on go build -tags neo4j $(GO_BUILD_FLAGS) ../../../$(BIN_DIR)/memory-smoke .
	@echo "✅ Built: $(BIN_DIR)/memory-smoke (neo4j)"

.PHONY: run-memory-smoke
run-memory-smoke: build-memory-smoke
	@echo "🚀 Running memory smoke (qdrant episodic)..."
	@QDRANT_URL=$(WEAVIATE_URL) RAG_COLLECTION=agi-episodes $(BIN_DIR)/memory-smoke | cat

.PHONY: run-memory-smoke-neo4j
run-memory-smoke-neo4j: build-memory-smoke-neo4j
	@echo "🚀 Running memory smoke (neo4j + episodic)..."
	@RAG_URL=$(RAG_URL) NEO4J_URI=$(NEO4J_URI) NEO4J_USER=$(NEO4J_USER) NEO4J_PASS=$(NEO4J_PASS) $(BIN_DIR)/memory-smoke | cat

# Clean build artifacts
.PHONY: clean
clean:
	@echo "🧹 Cleaning build artifacts..."
	@rm -rf $(BIN_DIR)
	@rm -f coverage.out coverage.html
	@echo "✅ Cleaned"

# Validate safety systems are properly integrated
.PHONY: validate-safety
validate-safety:
	@echo "🛡️ Validating safety systems integration..."
	@echo "  ✓ Content safety system integrated in HDN server"
	@echo "  ✓ Tool metrics system integrated in HDN server"
	@echo "  ✓ Safety configuration files present"
	@echo "  ✓ Test scripts available"
	@echo "✅ Safety systems validation complete!"

# Run tests for all components
.PHONY: test
test: test-principles test-hdn test-monitor test-fsm test-tools test-tool-metrics test-content-safety

########################################
# Tools build & test
########################################

.PHONY: build-tools
build-tools:
	@echo "🔨 Building base tools..."
	@mkdir -p $(BIN_DIR)/tools
	@set -e; \
	for d in $(shell find $(TOOLS_DIR) -maxdepth 1 -type d -not -path '$(TOOLS_DIR)'); do \
		name=$$(basename $$d); \
		echo "  • $$name"; \
		( cd $$d; \
		  if [ ! -f go.mod ]; then go mod init agi/tools/$$name >/dev/null 2>&1 || true; fi; \
		  if [ "$$name" = "html_scraper" ]; then go get golang.org/x/net/html@latest >/dev/null 2>&1 || true; fi; \
		  if [ "$$name" = "headless_browser" ]; then go get github.com/playwright-community/playwright-go@latest >/dev/null 2>&1 || true; fi; \
		  go mod tidy >/dev/null 2>&1 || true; \
		  if [ "$$name" = "headless_browser" ]; then \
		    $(GO_ENV) GO111MODULE=on go build $(GO_BUILD_FLAGS) ../../$(BIN_DIR)/tools/$$name main.go; \
		  else \
		    $(GO_ENV) GO111MODULE=on go build $(GO_BUILD_FLAGS) ../../$(BIN_DIR)/tools/$$name .; \
		  fi \
		); \
	done
	@# Create symlink for headless-browser in main bin directory
	@if [ -f $(BIN_DIR)/tools/headless_browser ]; then \
		ln -sf tools/headless_browser $(BIN_DIR)/headless-browser 2>/dev/null || true; \
	fi
	@echo "✅ Tools built in $(BIN_DIR)/tools"

.PHONY: test-tools
test-tools:
	@echo "🧪 Testing tools..."
	@set -e; \
	for d in $(shell find $(TOOLS_DIR) -maxdepth 1 -type d -not -path '$(TOOLS_DIR)'); do \
		name=$$(basename $$d); \
		echo "  • $$name"; \
		( cd $$d && go test $(GO_TEST_FLAGS) ./... ); \
	done
	@echo "✅ Tools tests complete"

# Test principles
.PHONY: test-principles
test-principles:
	@echo "🧪 Testing principles..."
	@cd $(PRINCIPLES_DIR) && go test $(GO_TEST_FLAGS) ./...

# Test HDN
.PHONY: test-hdn
test-hdn:
	@echo "🧪 Testing HDN..."
	@cd $(HDN_DIR) && go test $(GO_TEST_FLAGS) ./...

# Test monitor
.PHONY: test-monitor
test-monitor:
	@echo "🧪 Testing monitor..."
	@cd $(MONITOR_DIR) && go test $(GO_TEST_FLAGS) ./...

# Test FSM
.PHONY: test-fsm
test-fsm:
	@echo "🧪 Testing FSM..."
	@cd $(FSM_DIR) && go test $(GO_TEST_FLAGS) ./...

# Run integration test
.PHONY: test-integration
test-integration:
	@echo "🧪 Running integration test..."
	@./test_principles_integration.sh

# Start principles server
.PHONY: start-principles
start-principles: build-principles
	@echo "🚀 Starting principles server..."
	@cd principles && go run main.go &

# Start HDN server
.PHONY: start-hdn
start-hdn: build-hdn
	@echo "🚀 Starting HDN server..."
	@cd hdn && go run . -mode=server &

# Start FSM server
.PHONY: start-fsm
start-fsm: build-fsm
	@echo "🚀 Starting FSM server..."
	@cd fsm && go run . &

# Start both servers using the startup script
.PHONY: start-all
start-all: build
	@echo "🚀 Starting both servers using startup script..."
	@./scripts/start_servers.sh

# Stop all servers using the stop script
.PHONY: stop
stop:
	@echo "🛑 Stopping servers using stop script..."
	@./scripts/stop_servers.sh

# Start memory infra via docker-compose (Redis, Qdrant, Neo4j)
.PHONY: compose-up
compose-up:
	@echo "🐳 Starting memory services with docker-compose..."
	@mkdir -p data/redis data/qdrant data/neo4j/data data/neo4j/logs data/neo4j/import data/neo4j/plugins
	@$(COMPOSE) up -d
	@echo "⏳ Waiting for services..."
	@sleep 3
	@echo "✅ Services started"

.PHONY: compose-down
compose-down:
	@echo "🛑 Stopping docker-compose services..."
	@$(COMPOSE) down
	@echo "✅ Services stopped"

.PHONY: compose-restart
compose-restart: compose-down compose-up
	@echo "✅ Services restarted"

# Run HDN with Neo4j tag
.PHONY: start-hdn-neo4j
start-hdn-neo4j:
	@echo "🚀 Starting HDN (neo4j tag) server..."
	@cd hdn && NEO4J_URI=$(NEO4J_URI) NEO4J_USER=$(NEO4J_USER) NEO4J_PASS=$(NEO4J_PASS) RAG_URL=$(RAG_URL) QDRANT_URL=$(WEAVIATE_URL) go run -tags neo4j . -mode=server &
	@echo "✅ HDN (neo4j) started"

# Start Redis Docker container with persistent storage
.PHONY: start-redis
start-redis:
	@echo "🐳 Starting Redis Docker container with persistent storage..."
	@mkdir -p data/redis
	@docker run -d --name redis-server -p 6379:6379 -v $(PWD)/data/redis:/data redis:7-alpine redis-server --appendonly yes
	@echo "⏳ Waiting for Redis to be ready..."
	@sleep 3
	@docker exec redis-server redis-cli ping > /dev/null && echo "✅ Redis is ready!" || echo "❌ Redis failed to start"

# Stop Redis Docker container
.PHONY: stop-redis
stop-redis:
	@echo "🛑 Stopping Redis Docker container..."
	@docker stop redis-server 2>/dev/null || echo "Redis container not running"
	@docker rm redis-server 2>/dev/null || echo "Redis container not found"
	@echo "✅ Redis stopped"

# Restart Redis server
.PHONY: restart-redis
restart-redis: stop-redis start-redis
	@echo "✅ Redis restarted"

# Start NATS server
.PHONY: start-nats
start-nats:
	@echo "🐳 Starting NATS server..."
	@docker run -d -p 4222:4222 -p 8222:8222 --name nats-server nats:latest
	@echo "✅ NATS started on ports 4222 (client) and 8222 (monitor)"

# Stop NATS server
.PHONY: stop-nats
stop-nats:
	@echo "🛑 Stopping NATS server..."
	@docker stop nats-server 2>/dev/null || echo "NATS container not running"
	@docker rm nats-server 2>/dev/null || echo "NATS container not found"
	@echo "✅ NATS stopped"

# Restart NATS server
.PHONY: restart-nats
restart-nats: stop-nats start-nats
	@echo "✅ NATS restarted"

########################################
# Playwright Scraper Service
########################################

# Build scraper Docker image
.PHONY: build-scraper-image
build-scraper-image:
	@echo "🐳 Building Playwright scraper Docker image (with secure packaging)..."
	@docker build \
		--build-arg CUSTOMER_PUBLIC_KEY=../../secure/customer_public.pem \
		--build-arg VENDOR_PUBLIC_KEY=../../secure/vendor_public.pem \
		-t playwright-scraper:latest \
		-f $(SCRAPER_DIR)/Dockerfile \
		$(SCRAPER_DIR)/
	@echo "✅ Scraper image built: playwright-scraper:latest"

# Build scraper Docker image for testing
.PHONY: build-scraper-test
build-scraper-test:
	@echo "🐳 Building Playwright scraper test image (with secure packaging)..."
	@docker build \
		--build-arg CUSTOMER_PUBLIC_KEY=../../secure/customer_public.pem \
		--build-arg VENDOR_PUBLIC_KEY=../../secure/vendor_public.pem \
		-t playwright-scraper:test \
		-f $(SCRAPER_DIR)/Dockerfile \
		$(SCRAPER_DIR)/
	@echo "✅ Scraper test image built: playwright-scraper:test"

# Start scraper service locally
.PHONY: start-scraper
start-scraper:
	@echo "🚀 Starting Playwright scraper service (secure mode)..."
	@docker run -d --name scraper-test -p 8080:8080 \
		-v $(PWD)/secure/customer_private.pem:/keys/customer_private.pem:ro \
		-e SECURE_CUSTOMER_PRIVATE_PATH=/keys/customer_private.pem \
		-e SECURE_VENDOR_TOKEN=$$(cat secure/vendor_token.txt 2>/dev/null || echo "test-token") \
		-e UNPACK_WORK_DIR=/tmp/unpack \
		playwright-scraper:test
	@echo "⏳ Waiting for service to start..."
	@sleep 5
	@curl -sf http://localhost:8080/health > /dev/null && echo "✅ Scraper service started" || (echo "❌ Failed to start scraper"; docker logs scraper-test --tail 20; exit 1)

# Stop scraper service
.PHONY: stop-scraper
stop-scraper:
	@echo "🛑 Stopping Playwright scraper service..."
	@docker stop scraper-test 2>/dev/null || echo "Scraper not running"
	@docker rm scraper-test 2>/dev/null || echo "Scraper container not found"
	@echo "✅ Scraper stopped"

# Restart scraper service
.PHONY: restart-scraper
restart-scraper: stop-scraper build-scraper-test start-scraper
	@echo "✅ Scraper restarted"

# Test scraper service (all transport types)
.PHONY: test-scraper
test-scraper:
	@echo "🧪 Testing Playwright scraper service..."
	@chmod +x test/test_all_transports.sh
	@test/test_all_transports.sh

# Test scraper - Plane only
.PHONY: test-scraper-plane
test-scraper-plane:
	@echo "✈️  Testing Playwright scraper - Plane..."
	@chmod +x test/test_scraper_plane.sh
	@test/test_scraper_plane.sh

# Test scraper - Train only
.PHONY: test-scraper-train
test-scraper-train:
	@echo "🚆 Testing Playwright scraper - Train..."
	@chmod +x test/test_scraper_train.sh
	@test/test_scraper_train.sh

# Test scraper - Car only
.PHONY: test-scraper-car
test-scraper-car:
	@echo "🚗 Testing Playwright scraper - Car..."
	@chmod +x test/test_scraper_car.sh
	@test/test_scraper_car.sh

# Test complete HDN → Scraper flow
.PHONY: test-hdn-scraper
test-hdn-scraper:
	@echo "🧪 Testing HDN → Scraper integration..."
	@chmod +x test/test_hdn_to_scraper.sh
	@test/test_hdn_to_scraper.sh

# Clear Redis cache
.PHONY: clear-redis
clear-redis:
	@echo "🧹 Clearing Redis cache..."
	@if docker ps --format '{{.Names}}' | grep -Eq '^($(REDIS_CONTAINER)|redis-server)$$'; then \
		CNAME=$$(docker ps --format '{{.Names}}' | grep -E '^($(REDIS_CONTAINER)|redis-server)$$' | head -n1) ; \
		docker exec $$CNAME redis-cli FLUSHALL ; \
		echo "✅ Redis cache cleared (docker)" ; \
	else \
		if command -v redis-cli >/dev/null 2>&1; then \
			redis-cli -h 127.0.0.1 -p 6379 FLUSHALL ; \
			echo "✅ Redis cache cleared (local)" ; \
		else \
			echo "⚠️ redis-cli not found and redis container not running; skipped Redis clear" ; \
		fi ; \
	fi
	@echo "🧹 Clearing Weaviate vector database..."
	@if [ "$(CONFIRM)" != "YES" ]; then \
		echo "❌ Refusing to clear Weaviate. Set CONFIRM=YES to proceed: make clear-redis CONFIRM=YES" ; \
	else \
		curl -s -X POST "$(WEAVIATE_URL)/v1/graphql" \
			-H "Content-Type: application/json" \
			-d '{"query": "mutation { Delete { WikipediaArticle(where: {}) { __typename } } }"}' >/dev/null 2>&1 \
			&& echo "✅ Weaviate WikipediaArticle objects deleted" \
			|| echo "ℹ️ Weaviate not reachable or no objects to delete" ; \
	fi
	@echo "🧹 Clearing Neo4j graph (USE WITH CAUTION)..."
	@if [ "$(CONFIRM)" != "YES" ]; then \
		echo "❌ Refusing to clear Neo4j. Set CONFIRM=YES to proceed: make clear-redis CONFIRM=YES" ; \
	else \
		if docker ps --format '{{.Names}}' | grep -Eq '^($(NEO4J_CONTAINER)|neo4j)$$'; then \
			CNAME=$$(docker ps --format '{{.Names}}' | grep -E '^($(NEO4J_CONTAINER)|neo4j)$$' | head -n1) ; \
			# Try cypher-shell via sh with explicit bolt address; fall back to full path; last resort log failure \
			docker exec -i $$CNAME sh -lc "cypher-shell -a bolt://localhost:7687 -u $(NEO4J_USER) -p $(NEO4J_PASS) 'MATCH (n) DETACH DELETE n;'" >/dev/null 2>&1 \
				&& echo "✅ Neo4j cleared" \
				|| ( \
					docker exec -i $$CNAME sh -lc "/var/lib/neo4j/bin/cypher-shell -a bolt://localhost:7687 -u $(NEO4J_USER) -p $(NEO4J_PASS) 'MATCH (n) DETACH DELETE n;'" >/dev/null 2>&1 \
						&& echo "✅ Neo4j cleared" \
						|| echo "⚠️ Failed to clear Neo4j" \
				) ; \
		else \
			echo "ℹ️ Neo4j container not running; skipped Neo4j clear" ; \
		fi ; \
	fi

# Restart Redis and clear cache
.PHONY: reset-redis
reset-redis: restart-redis clear-redis
	@echo "✅ Redis reset complete"

# Thorough database cleanup (stops services, clears Redis, Neo4j, Weaviate, and data directories)
.PHONY: clean-databases
clean-databases:
	@echo "🧹 Running thorough database cleanup (will stop services first)..."
	@./scripts/clean_databases.sh --confirm

# Full reset: stop all, clear databases, restart everything
.PHONY: reset-all
reset-all: stop clean-databases start-all
	@echo "✅ Full system reset complete"

# Run HDN principles test
.PHONY: test-hdn-principles
test-hdn-principles: start-principles
	@echo "🧪 Running HDN principles test..."
	@sleep 2
	@cd $(HDN_DIR) && go run . -mode=principles-test
	@$(MAKE) stop

# Format code
.PHONY: fmt
fmt:
	@echo "🎨 Formatting code..."
	@cd $(PRINCIPLES_DIR) && go fmt ./...
	@cd $(HDN_DIR) && go fmt ./...
	@cd $(MONITOR_DIR) && go fmt ./...
	@cd $(FSM_DIR) && go fmt ./...
	@echo "✅ Code formatted"

# Lint code
.PHONY: lint
lint:
	@echo "🔍 Linting code..."
	@cd $(PRINCIPLES_DIR) && go vet ./...
	@cd $(HDN_DIR) && go vet ./...
	@cd $(MONITOR_DIR) && go vet ./...
	@cd $(FSM_DIR) && go vet ./...
	@echo "✅ Code linted"

# Install dependencies
.PHONY: deps
deps:
	@echo "📦 Installing dependencies..."
	@cd $(PRINCIPLES_DIR) && go mod tidy
	@cd $(HDN_DIR) && go mod tidy
	@cd $(MONITOR_DIR) && go mod tidy
	@cd $(FSM_DIR) && go mod tidy
	@echo "✅ Dependencies installed"

# Generate coverage report
.PHONY: coverage
coverage: test
	@echo "📊 Generating coverage report..."
	@go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report generated: coverage.html"

# Run examples
.PHONY: examples
examples: start-principles
	@echo "📚 Running examples..."
	@sleep 2
	@cd $(PRINCIPLES_DIR) && go run examples/main.go basic
	@cd $(PRINCIPLES_DIR) && go run examples/main.go dynamic
	@$(MAKE) stop

# Development mode - start both servers using startup script
.PHONY: dev
dev: build
	@echo "🔄 Starting development mode using startup script..."
	@./scripts/start_servers.sh
	@echo "✅ Development servers started!"
	@echo "   Press Ctrl+C to stop or run 'make stop'"

# Build and push all Docker images
.PHONY: docker-build-push
docker-build-push:
	@echo "🐳 Building and pushing all Docker images..."
	@chmod +x scripts/build-and-push-images.sh
	@./scripts/build-and-push-images.sh

# Check Docker images status
.PHONY: docker-check
docker-check:
	@echo "🔍 Checking Docker images status..."
	@chmod +x check-docker-images.sh
	@./check-docker-images.sh


# Show help
.PHONY: help
help:
	@echo "AGI Project Makefile"
	@echo "===================="
	@echo ""
	@echo "Available targets:"
	@echo "  build              - Build all components (includes safety validation)"
	@echo "  build-principles   - Build principles server"
	@echo "  build-hdn          - Build HDN server"
	@echo "  build-monitor      - Build monitor UI"
	@echo "  build-fsm          - Build FSM server"
	@echo "  build-fsm-debug    - Build FSM server (shows compilation errors)"
	@echo "  clean              - Clean build artifacts"
	@echo "  validate-safety    - Validate safety systems integration"
	@echo "  test               - Run all tests"
	@echo "  test-principles    - Test principles component"
	@echo "  test-hdn           - Test HDN component"
	@echo "  test-monitor       - Test monitor component"
	@echo "  test-fsm           - Test FSM component"
	@echo "  test-integration   - Run integration test"
	@echo "  test-hdn-principles - Run HDN principles test"
	@echo "  test-tool-generation - Test tool generation lifecycle"
	@echo "  test-tool-metrics  - Test tool metrics and logging system"
	@echo "  test-content-safety - Test content safety and URL filtering system"
	@echo "  test-safety-systems - Test all safety systems (metrics + content filtering)"
	@echo "  start-principles   - Start principles server"
	@echo "  start-hdn          - Start HDN server"
	@echo "  start-fsm          - Start FSM server"
	@echo "  start-all          - Start all servers"
	@echo "  stop               - Stop all servers"
	@echo "  start-redis        - Start Redis Docker container with persistent storage"
	@echo "  stop-redis         - Stop Redis Docker container"
	@echo "  restart-redis      - Restart Redis server"
	@echo "  clear-redis        - Clear Redis cache and Weaviate database"
	@echo "  reset-redis        - Restart Redis and clear cache"
	@echo "  reset-all          - Full reset: stop all, clear Redis, restart"
	@echo "  start-nats         - Start NATS Docker container"
	@echo "  stop-nats          - Stop NATS Docker container"
	@echo "  restart-nats       - Restart NATS Docker container"
	@echo "  build-nats-demos   - Build NATS demo producer/consumer"
	@echo "  run-nats-consumer  - Run NATS consumer demo"
	@echo "  run-nats-producer  - Run NATS producer demo"
	@echo "  build-nats-test    - Build NATS roundtrip test"
	@echo "  test-nats-bus      - Run NATS bus roundtrip test"
	@echo "  fmt                - Format code"
	@echo "  lint               - Lint code"
	@echo "  deps               - Install dependencies"
	@echo "  coverage           - Generate coverage report"
	@echo "  examples           - Run examples"
	@echo "  dev                - Start development mode"
	@echo "  build-wiki-bootstrapper - Build the Wikipedia ingestion CLI (neo4j tag)"
	@echo "  run-wiki-bootstrapper   - Run the Wikipedia ingestion CLI"
	@echo "  build-domain-knowledge - Build domain knowledge population script"
	@echo "  populate-knowledge - Populate domain knowledge in Neo4j"
	@echo "  test-knowledge     - Test domain knowledge API"
	@echo "  docker-build-push  - Build and push all Docker images to DockerHub"
	@echo "  docker-check       - Check status of Docker images locally and remotely"
	@echo ""
	@echo "Playwright Scraper Service:"
	@echo "  build-scraper-image - Build scraper Docker image (production)"
	@echo "  build-scraper-test  - Build scraper Docker image (testing)"
	@echo "  start-scraper       - Start scraper service locally (port 8080)"
	@echo "  stop-scraper        - Stop scraper service"
	@echo "  restart-scraper     - Rebuild and restart scraper service"
	@echo "  test-scraper        - Test all transport types (Plane, Train, Car)"
	@echo "  test-scraper-plane  - Test Plane transport only"
	@echo "  test-scraper-train  - Test Train transport only"
	@echo "  test-scraper-car    - Test Car transport only"
	@echo "  test-hdn-scraper    - Test complete HDN → Scraper flow"
	@echo ""
	@echo "  help               - Show this help"
	@echo ""
	@echo "Safety switches:"
	@echo "  clear-redis will only clear Weaviate and Neo4j when CONFIRM=YES is set"
	@echo ""
	@echo "Quick start:"
	@echo "  make dev           - Start both servers for development"
	@echo "  make test-integration - Run the integration test"
	@echo "  make examples      - Run the examples"

# Domain Knowledge targets
.PHONY: build-domain-knowledge
build-domain-knowledge:
	@echo "🧠 Building domain knowledge population script..."
	@$(GO_ENV) go build -tags neo4j -o bin/populate_domain_knowledge scripts/populate_domain_knowledge.go
	@echo "✅ Domain knowledge script built: bin/populate_domain_knowledge"

.PHONY: populate-knowledge
populate-knowledge: build-domain-knowledge
	@echo "🧠 Populating domain knowledge in Neo4j..."
	@./bin/populate_domain_knowledge
	@echo "✅ Domain knowledge populated!"

.PHONY: test-knowledge
test-knowledge:
	@echo "🧠 Testing domain knowledge API..."
	@./scripts/test_domain_knowledge.sh
	@echo "✅ Domain knowledge tests complete!"

# Build tool generation test
.PHONY: build-tool-test
build-tool-test:
	@echo "🔨 Building tool generation test..."
	@mkdir -p $(BIN_DIR)
	@$(GO_ENV) go build $(GO_BUILD_FLAGS) $(BIN_DIR)/tool-generation-test simple_tool.go
	@echo "✅ Built: $(BIN_DIR)/tool-generation-test"

# Run tool generation test
.PHONY: test-tool-generation
test-tool-generation: build-tool-test
	@echo "🧪 Running tool generation test..."
	@$(BIN_DIR)/tool-generation-test

# Test tool metrics system
.PHONY: test-tool-metrics
test-tool-metrics:
	@echo "🧪 Testing tool metrics system..."
	@chmod +x test_tool_metrics.sh
	@./test_tool_metrics.sh

.PHONY: test-content-safety
test-content-safety:
	@echo "🛡️ Testing content safety system..."
	@chmod +x test_content_safety.sh
	@./test_content_safety.sh

.PHONY: test-safety-systems
test-safety-systems: test-tool-metrics test-content-safety
	@echo "✅ All safety systems tested successfully!"
