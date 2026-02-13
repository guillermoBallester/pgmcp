package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/guillermoballestersasso/pgmcp/internal/core/ports"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func RegisterTools(s *server.MCPServer, explorer ports.SchemaExplorer, executor ports.QueryExecutor) {
	// list_tables
	s.AddTool(
		mcp.NewTool("list_tables",
			mcp.WithDescription("List all tables in the database with their schemas, estimated row counts, and comments"),
		),
		listTablesHandler(explorer),
	)

	// describe_table
	s.AddTool(
		mcp.NewTool("describe_table",
			mcp.WithDescription("Describe a table's columns, primary keys, foreign keys, indexes, and comments"),
			mcp.WithString("table_name",
				mcp.Required(),
				mcp.Description("Name of the table to describe"),
			),
		),
		describeTableHandler(explorer),
	)

	// query
	s.AddTool(
		mcp.NewTool("query",
			mcp.WithDescription("Execute a SQL query against the database. Results are returned as a JSON array of objects. Queries run in a read-only transaction with a row limit and timeout enforced server-side."),
			mcp.WithString("sql",
				mcp.Required(),
				mcp.Description("SQL query to execute"),
			),
		),
		queryHandler(executor),
	)
}

func listTablesHandler(explorer ports.SchemaExplorer) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tables, err := explorer.ListTables(ctx)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to list tables: %v", err)), nil
		}

		data, err := json.Marshal(tables)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal results: %v", err)), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}

func describeTableHandler(explorer ports.SchemaExplorer) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tableName, ok := request.GetArguments()["table_name"].(string)
		if !ok || tableName == "" {
			return mcp.NewToolResultError("table_name is required"), nil
		}

		detail, err := explorer.DescribeTable(ctx, tableName)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to describe table: %v", err)), nil
		}

		data, err := json.Marshal(detail)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal results: %v", err)), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}

func queryHandler(executor ports.QueryExecutor) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sql, ok := request.GetArguments()["sql"].(string)
		if !ok || sql == "" {
			return mcp.NewToolResultError("sql is required"), nil
		}

		results, err := executor.Execute(ctx, sql)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
		}

		data, err := json.Marshal(results)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal results: %v", err)), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}
