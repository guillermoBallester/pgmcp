package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"io"
	"log/slog"

	"github.com/guillermoBallester/isthmus/internal/core/domain"
	"github.com/guillermoBallester/isthmus/internal/core/port"
	"github.com/guillermoBallester/isthmus/internal/core/service"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock SchemaExplorer ---

type mockExplorer struct {
	schemas []port.SchemaInfo
	tables  []port.TableInfo
	detail  *port.TableDetail
	err     error
}

func (m *mockExplorer) ListSchemas(_ context.Context) ([]port.SchemaInfo, error) {
	return m.schemas, m.err
}

func (m *mockExplorer) ListTables(_ context.Context) ([]port.TableInfo, error) {
	return m.tables, m.err
}

func (m *mockExplorer) DescribeTable(_ context.Context, _, _ string) (*port.TableDetail, error) {
	return m.detail, m.err
}

// --- mock QueryExecutor ---

type mockExecutor struct {
	result []map[string]any
	err    error
}

func (m *mockExecutor) Execute(_ context.Context, _ string) ([]map[string]any, error) {
	return m.result, m.err
}

// --- helpers ---

func callTool(t *testing.T, s *server.MCPServer, toolName string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx := context.Background()
	session := server.NewInProcessSession("test", nil)
	require.NoError(t, s.RegisterSession(ctx, session))
	sessionCtx := s.WithContext(ctx, session)

	// Initialize session.
	initBytes, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "init", "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
		},
	})
	s.HandleMessage(sessionCtx, initBytes)

	// Call tool.
	reqBytes, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "call-1", "method": "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	})
	resp := s.HandleMessage(sessionCtx, reqBytes)
	respBytes, _ := json.Marshal(resp)

	var rpc struct {
		Result *mcp.CallToolResult       `json:"result"`
		Error  *struct{ Message string } `json:"error,omitempty"`
	}
	require.NoError(t, json.Unmarshal(respBytes, &rpc))
	require.Nil(t, rpc.Error, "unexpected RPC error: %v", rpc.Error)
	require.NotNil(t, rpc.Result)
	return rpc.Result
}

func toolText(result *mcp.CallToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		return ""
	}
	return tc.Text
}

func setupServer(explorer *mockExplorer, executor *mockExecutor) *server.MCPServer {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	explorerSvc := service.NewExplorerService(explorer)

	var querySvc *service.QueryService
	if executor != nil {
		querySvc = service.NewQueryService(domain.NewQueryValidator(), executor, logger)
	}

	s := server.NewMCPServer("test", "0.1.0", server.WithToolCapabilities(true))
	RegisterTools(s, explorerSvc, querySvc)
	return s
}

// --- tests ---

func TestListSchemas_HappyPath(t *testing.T) {
	explorer := &mockExplorer{
		schemas: []port.SchemaInfo{{Name: "public"}, {Name: "auth"}},
	}
	s := setupServer(explorer, nil)

	result := callTool(t, s, "list_schemas", nil)
	text := toolText(result)

	var schemas []port.SchemaInfo
	require.NoError(t, json.Unmarshal([]byte(text), &schemas))
	assert.Len(t, schemas, 2)
	assert.Equal(t, "public", schemas[0].Name)
}

func TestListSchemas_Error(t *testing.T) {
	explorer := &mockExplorer{err: fmt.Errorf("permission denied")}
	s := setupServer(explorer, nil)

	result := callTool(t, s, "list_schemas", nil)
	assert.True(t, result.IsError)
	assert.Contains(t, toolText(result), "permission denied")
}

func TestListTables_HappyPath(t *testing.T) {
	explorer := &mockExplorer{
		tables: []port.TableInfo{
			{Schema: "public", Name: "users", Type: "table", RowEstimate: 100},
		},
	}
	s := setupServer(explorer, nil)

	result := callTool(t, s, "list_tables", nil)
	text := toolText(result)

	var tables []port.TableInfo
	require.NoError(t, json.Unmarshal([]byte(text), &tables))
	require.Len(t, tables, 1)
	assert.Equal(t, "users", tables[0].Name)
}

func TestDescribeTable_HappyPath(t *testing.T) {
	explorer := &mockExplorer{
		detail: &port.TableDetail{
			Schema: "public",
			Name:   "users",
			Columns: []port.ColumnInfo{
				{Name: "id", DataType: "uuid", IsPrimaryKey: true},
				{Name: "email", DataType: "text"},
			},
		},
	}
	s := setupServer(explorer, nil)

	result := callTool(t, s, "describe_table", map[string]any{"table_name": "users"})
	text := toolText(result)

	var detail port.TableDetail
	require.NoError(t, json.Unmarshal([]byte(text), &detail))
	assert.Equal(t, "users", detail.Name)
	assert.Len(t, detail.Columns, 2)
}

func TestDescribeTable_MissingTableName(t *testing.T) {
	s := setupServer(&mockExplorer{}, nil)

	result := callTool(t, s, "describe_table", map[string]any{})
	assert.True(t, result.IsError)
	assert.Contains(t, toolText(result), "table_name is required")
}

func TestDescribeTable_Error(t *testing.T) {
	explorer := &mockExplorer{err: fmt.Errorf("table not found")}
	s := setupServer(explorer, nil)

	result := callTool(t, s, "describe_table", map[string]any{"table_name": "nonexistent"})
	assert.True(t, result.IsError)
	assert.Contains(t, toolText(result), "table not found")
}

func TestQuery_HappyPath(t *testing.T) {
	executor := &mockExecutor{
		result: []map[string]any{{"id": 1, "name": "alice"}},
	}
	// QueryService with nil validator — we test the tool handler, not the validator.
	s := setupServer(&mockExplorer{}, executor)

	result := callTool(t, s, "query", map[string]any{"sql": "SELECT id, name FROM users"})
	text := toolText(result)

	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &rows))
	require.Len(t, rows, 1)
	assert.Equal(t, "alice", rows[0]["name"])
}

func TestQuery_MissingSQL(t *testing.T) {
	s := setupServer(&mockExplorer{}, &mockExecutor{})

	result := callTool(t, s, "query", map[string]any{})
	assert.True(t, result.IsError)
	assert.Contains(t, toolText(result), "sql is required")
}

func TestQuery_ExecutorError(t *testing.T) {
	executor := &mockExecutor{err: fmt.Errorf("connection timeout")}
	s := setupServer(&mockExplorer{}, executor)

	result := callTool(t, s, "query", map[string]any{"sql": "SELECT 1"})
	assert.True(t, result.IsError)
	assert.Contains(t, toolText(result), "connection timeout")
}
