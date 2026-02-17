package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/guillermoballestersasso/pgmcp/pkg/core/service"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server metadata
const (
	serverName    = "pgmcp"
	serverVersion = "0.1.0"
)

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

func RegisterTools(s *server.MCPServer, explorer *service.ExplorerService, query *service.QueryService, logger *slog.Logger) {
	s.AddTool(
		mcp.NewTool("list_schemas",
			mcp.WithDescription(descListSchemas),
		),
		listSchemasHandler(explorer, logger),
	)

	s.AddTool(
		mcp.NewTool("list_tables",
			mcp.WithDescription(descListTables),
		),
		listTablesHandler(explorer, logger),
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
		describeTableHandler(explorer, logger),
	)

	s.AddTool(
		mcp.NewTool("query",
			mcp.WithDescription(descQuery),
			mcp.WithString("sql",
				mcp.Required(),
				mcp.Description(descQueryParam),
			),
		),
		queryHandler(query, logger),
	)
}

func listSchemasHandler(explorer *service.ExplorerService, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		logger.InfoContext(ctx, "tool call received",
			slog.String("rpc.method", "tools/call"),
			slog.String("mcp.tool", "list_schemas"),
		)

		schemas, err := explorer.ListSchemas(ctx)
		if err != nil {
			logger.ErrorContext(ctx, "tool call failed",
				slog.String("mcp.tool", "list_schemas"),
				slog.Duration("duration", time.Since(start)),
				slog.String("error.type", "tool_error"),
			)
			return mcp.NewToolResultError(fmt.Sprintf("failed to list schemas: %v", err)), nil
		}

		data, err := json.Marshal(schemas)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal results: %v", err)), nil
		}

		logger.InfoContext(ctx, "tool call completed",
			slog.String("mcp.tool", "list_schemas"),
			slog.Int("db.response.rows", len(schemas)),
			slog.Duration("duration", time.Since(start)),
		)
		return mcp.NewToolResultText(string(data)), nil
	}
}

func listTablesHandler(explorer *service.ExplorerService, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		logger.InfoContext(ctx, "tool call received",
			slog.String("rpc.method", "tools/call"),
			slog.String("mcp.tool", "list_tables"),
		)

		tables, err := explorer.ListTables(ctx)
		if err != nil {
			logger.ErrorContext(ctx, "tool call failed",
				slog.String("mcp.tool", "list_tables"),
				slog.Duration("duration", time.Since(start)),
				slog.String("error.type", "tool_error"),
			)
			return mcp.NewToolResultError(fmt.Sprintf("failed to list tables: %v", err)), nil
		}

		data, err := json.Marshal(tables)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal results: %v", err)), nil
		}

		logger.InfoContext(ctx, "tool call completed",
			slog.String("mcp.tool", "list_tables"),
			slog.Int("db.response.rows", len(tables)),
			slog.Duration("duration", time.Since(start)),
		)
		return mcp.NewToolResultText(string(data)), nil
	}
}

func describeTableHandler(explorer *service.ExplorerService, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tableName, ok := request.GetArguments()["table_name"].(string)
		if !ok || tableName == "" {
			return mcp.NewToolResultError("table_name is required"), nil
		}

		start := time.Now()
		logger.InfoContext(ctx, "tool call received",
			slog.String("rpc.method", "tools/call"),
			slog.String("mcp.tool", "describe_table"),
			slog.String("db.collection.name", tableName),
		)

		schema, _ := request.GetArguments()["schema"].(string)

		detail, err := explorer.DescribeTable(ctx, schema, tableName)
		if err != nil {
			logger.ErrorContext(ctx, "tool call failed",
				slog.String("mcp.tool", "describe_table"),
				slog.String("db.collection.name", tableName),
				slog.Duration("duration", time.Since(start)),
				slog.String("error.type", "tool_error"),
			)
			return mcp.NewToolResultError(fmt.Sprintf("failed to describe table: %v", err)), nil
		}

		data, err := json.Marshal(detail)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal results: %v", err)), nil
		}

		logger.InfoContext(ctx, "tool call completed",
			slog.String("mcp.tool", "describe_table"),
			slog.String("db.collection.name", tableName),
			slog.Duration("duration", time.Since(start)),
		)
		return mcp.NewToolResultText(string(data)), nil
	}
}

func queryHandler(query *service.QueryService, logger *slog.Logger) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sql, ok := request.GetArguments()["sql"].(string)
		if !ok || sql == "" {
			return mcp.NewToolResultError("sql is required"), nil
		}

		start := time.Now()
		logger.InfoContext(ctx, "tool call received",
			slog.String("rpc.method", "tools/call"),
			slog.String("mcp.tool", "query"),
		)

		results, err := query.Execute(ctx, sql)
		if err != nil {
			logger.ErrorContext(ctx, "tool call failed",
				slog.String("mcp.tool", "query"),
				slog.Duration("duration", time.Since(start)),
				slog.String("error.type", "tool_error"),
			)
			return mcp.NewToolResultError(fmt.Sprintf("query failed: %v", err)), nil
		}

		data, err := json.Marshal(results)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to marshal results: %v", err)), nil
		}

		logger.InfoContext(ctx, "tool call completed",
			slog.String("mcp.tool", "query"),
			slog.Int("db.response.rows", len(results)),
			slog.Duration("duration", time.Since(start)),
		)
		return mcp.NewToolResultText(string(data)), nil
	}
}
