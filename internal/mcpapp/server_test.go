package mcpapp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRegisterTools(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "rutracker-mcp", Version: "test"}, nil)
	Register(server, nil, t.TempDir())
}
