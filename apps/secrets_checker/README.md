# Secrets Checker

A high-efficiency Go program that monitors Git repositories for leaked secrets, integrating with PicoClaw's MCP secret scanner.

## Features

- **Concurrent Scanning**: Uses Go routines to scan multiple repositories and files in parallel.
- **Incremental Checks**: Tracks the last scanned commit for each repository to avoid redundant scans.
- **Auto-Discovery**: Automatically fetches and clones/updates repositories for configured GitHub users.
- **MCP Integration**: Calls the `mcp_hdn-server_secret_scanner` tool for robust secret detection.

## Local Testing

1.  Ensure you have an MCP server running at `http://localhost:8080/mcp` (or update `config.json`).
2.  Configure your target GitHub users in `config.json`.
3.  Run the program:
    ```bash
    go run main.go
    ```

## Configuration

Edit `config.json`:

```json
{
  "github_users": ["your-username", "another-username"],
  "monitor_dir": "repos",
  "mcp_server_url": "http://localhost:8080/mcp",
  "state_file": "state.json",
  "concurrency": 10
}
```

## K3s Deployment

The program is ready for k3s. Build and push the image, then deploy using your standard k3s workflow.

> [!NOTE]
> Ensure the container has network access to the MCP server.
