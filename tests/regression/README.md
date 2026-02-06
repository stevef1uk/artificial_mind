# Regression Testing Suite

This directory contains a standalone regression testing suite for the Artificial Mind project. It is designed to verify the core functionality of the Agent system, including the Finite State Machine (FSM), Hierarchical Decision Network (HDN), Tool integration, and Code Generation capabilities, without affecting your local development environment.

## Overview

The regression suite runs a complete, isolated environment using Docker Compose. It spins up:

*   **Core Services**: `hdn`, `fsm`, `scraper` (built from your local source).
*   **Infrastructure**: Ephemeral instances of `redis`, `nats`, `neo4j`, and `weaviate`.
*   **Mocks**:
    *   `mock-llm`: Simulates an OpenAI/Ollama compatible API for deterministic responses.
    *   `mock-mcp`: Simulates a Model Context Protocol tool server.
*   **Test Runner**: A Python-based runner that probes the services and asserts expected behaviors.

## Prerequisites

*   Docker Engine
*   Docker Compose
*   Go (for verifying local builds if needed)

## How to Run

From the project root directory:

```bash
./tests/regression/run.sh
```

This script will:
1.  Tear down any previous regression containers.
2.  Rebuild the regression-specific Docker images.
3.  Start the test infrastructure in the background.
4.  Run the tests via the `test-runner` container.
5.  Stream logs upon failure or print success message.
6.  Clean up the environment (unless debugging flags are changed in the script).

## Test Scenarios

The `test_runner.py` script performs the following checks:

1.  **Service Health**: Verifies all services (HDN, FSM, Scraper, Mocks) are reachable.
2.  **HDN State**: Checks if the HDN service is correctly maintaining state.
3.  **FSM Status**: Verifies the Finite State Machine is running and transitioning.
4.  **Agent Capabilities**: Confirms that agents and tools are correctly registered.
5.  **Code Generation & Execution**:
    *   Sends a code execution request to the HDN.
    *   Verifies that the HDN can spin up a Docker container.
    *   Checks if the code runs successfully and returns the expected output.
    *   *Note*: This verifies the `simple_docker_executor.go` shared directory implementation.

## Architecture & isolation

*   **Ports**: Uses a dedicated port range (10000+) to avoid conflicts with your development stack.
    *   HDN: 18080
    *   FSM: 18083
    *   Neo4j: 17474
    *   Redis: 16379
*   **Volumes**: Uses ephemeral volumes. No data is persisted to your local `./data` directory, ensuring your dev environment remains clean.
*   **Shared Directory**: Uses `/tmp/agi_regression` on the host to share code files between the HDN service and the executor containers it spawns.

## Troubleshooting

If the tests fail:

1.  The `run.sh` script will output the logs of the `test-runner` container.
2.  You can inspect full service logs by uncommenting the log dump lines in `tests/regression/run.sh`.
3.  To keep containers running after failure for manual inspection, comment out the `down -v` command in the failure block of `run.sh`.
