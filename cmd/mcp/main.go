// Command mcp serves the second brain over the Model Context Protocol using
// stdio. It is meant to be registered as an MCP server in opencode (or any
// other MCP client) running on the same machine as the brain HTTP API, which it
// wraps.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"desrosiers.org/brain/internal/mcp"
)

func main() {
	baseURL := os.Getenv("BRAIN_API_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8080"
	}
	client, err := mcp.NewClient(baseURL)
	if err != nil {
		log.Fatalf("mcp: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("brain MCP server (stdio) -> %s", baseURL)
	if err := mcp.New(client).Run(ctx, &sdkmcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
