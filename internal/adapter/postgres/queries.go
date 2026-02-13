package postgres

// queryListTables has one %s placeholder for the schema filter clause.
const queryListTables = `
	SELECT
		t.table_schema,
		t.table_name,
		COALESCE(s.n_live_tup, 0) AS row_estimate,
		COALESCE(pg_catalog.obj_description(
			(quote_ident(t.table_schema) || '.' || quote_ident(t.table_name))::regclass, 'pg_class'
		), '') AS comment
	FROM information_schema.tables t
	LEFT JOIN pg_stat_user_tables s
		ON s.schemaname = t.table_schema AND s.relname = t.table_name
	WHERE %s
		AND t.table_type = 'BASE TABLE'
	ORDER BY t.table_schema, t.table_name`

// queryTableMeta has one %s placeholder for the schema filter clause.
// $1 is always table_name; schema filter params start at $2.
const queryTableMeta = `
	SELECT t.table_schema,
		   COALESCE(pg_catalog.obj_description(
			   (quote_ident(t.table_schema) || '.' || quote_ident(t.table_name))::regclass, 'pg_class'
		   ), '')
	FROM information_schema.tables t
	WHERE t.table_name = $1
		AND %s
	LIMIT 1`

const queryColumns = `
	SELECT
		c.column_name,
		c.data_type,
		c.is_nullable = 'YES',
		COALESCE(c.column_default, ''),
		COALESCE(pg_catalog.col_description(
			(quote_ident(c.table_schema) || '.' || quote_ident(c.table_name))::regclass,
			c.ordinal_position
		), '')
	FROM information_schema.columns c
	WHERE c.table_schema = $1 AND c.table_name = $2
	ORDER BY c.ordinal_position`

const queryPrimaryKeys = `
	SELECT a.attname
	FROM pg_index i
	JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
	WHERE i.indrelid = (quote_ident($1) || '.' || quote_ident($2))::regclass
		AND i.indisprimary`

const queryForeignKeys = `
	SELECT
		tc.constraint_name,
		kcu.column_name,
		ccu.table_name AS referenced_table,
		ccu.column_name AS referenced_column
	FROM information_schema.table_constraints tc
	JOIN information_schema.key_column_usage kcu
		ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
	JOIN information_schema.constraint_column_usage ccu
		ON tc.constraint_name = ccu.constraint_name AND tc.table_schema = ccu.table_schema
	WHERE tc.constraint_type = 'FOREIGN KEY'
		AND tc.table_schema = $1
		AND tc.table_name = $2`

const queryIndexes = `
	SELECT
		indexname,
		indexdef,
		i.indisunique
	FROM pg_indexes pgi
	JOIN pg_class c ON c.relname = pgi.indexname
	JOIN pg_index i ON i.indexrelid = c.oid
	WHERE pgi.schemaname = $1 AND pgi.tablename = $2`
