package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/guillermoBallester/isthmus/pkg/core/service"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server metadata
const serverName = "isthmus"

// Tool descriptions
const (
	descListSchemas = "List all available database schemas. " +
		"Call this first to discover what schemas exist before listing tables or describing them."

	descListTables = "List all tables and views in the database with their schemas, types, estimated row counts, and comments. " +
		"Call this first to discover what tables and views are available before describing or querying them."

	descDescribeTable = "Describe a table's structure including columns (name, type, nullable, default, comment), " +
		"primary keys, foreign keys with referenced tables, and indexes. " +
		"Use this to understand a table's schema before writing queries. " +
		"Pay attention to foreign keys — they tell you how tables relate to each other for JOINs."

	descDescribeTableParam = "Name of the table to describe"

	descQuery = "Execute a read-only SQL query against the database and return results as a JSON array of objects. " +
		"A server-side row limit and query timeout are enforced. " +
		"Always use specific column names instead of SELECT *. " +
		"Use JOINs based on foreign keys discovered via describe_table."

	descQueryParam = "SQL query to execute (SELECT only)"
)

func RegisterTools(s *server.MCPServer, explorer *service.ExplorerService, query *service.QueryService) {
	s.AddTool(
		mcp.NewTool("list_schemas",
			mcp.WithDescription(descListSchemas),
		),
		listSchemasHandler(explorer),
	)

	s.AddTool(
		mcp.NewTool("list_tables",
			mcp.WithDescription(descListTables),
		),
		listTablesHandler(explorer),
	)

	s.AddTool(
		mcp.NewTool("describe_table",
			mcp.WithDescription(descDescribeTable),
			mcp.WithString("table_name",
				mcp.Required(),
				mcp.Description(descDescribeTableParam),
			),
			mcp.WithString("schema",
				mcp.Description("Schema name (optional, resolves automatically if omitted)"),
			),
		),
		describeTableHandler(explorer),
	)

	s.AddTool(
		mcp.NewTool("query",
			mcp.WithDescription(descQuery),
			mcp.WithString("sql",
				mcp.Required(),
				mcp.Description(descQueryParam),
			),
		),
		queryHandler(query),
	)
}

func listSchemasHandler(explorer *service.ExplorerService) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		schemas, err := explorer.ListSchemas(ctx)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to list schemas: %v", err)), nil
		}

		data, err := json.Marshal(schemas)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal results: %v", err)), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}

func listTablesHandler(explorer *service.ExplorerService) server.ToolHandlerFunc {
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

func describeTableHandler(explorer *service.ExplorerService) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tableName, ok := request.GetArguments()["table_name"].(string)
		if !ok || tableName == "" {
			return mcp.NewToolResultError("table_name is required"), nil
		}

		schema, _ := request.GetArguments()["schema"].(string)

		detail, err := explorer.DescribeTable(ctx, schema, tableName)
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

func queryHandler(query *service.QueryService) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sql, ok := request.GetArguments()["sql"].(string)
		if !ok || sql == "" {
			return mcp.NewToolResultError("sql is required"), nil
		}

		results, err := query.Execute(ctx, sql)
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
