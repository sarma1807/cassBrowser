package main

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

// QueryStats holds execution timing for a DuckDB query.
type QueryStats struct {
	LoadDuration  time.Duration // time to open DuckDB and register the parquet view
	QueryDuration time.Duration // time to execute the SQL and fetch all rows
}

// executeDuckDBQuery runs SQL against a parquet file via an in-memory DuckDB instance.
// The file is registered as a view named "virtual_table", so queries reference it as: SELECT * FROM virtual_table
func executeDuckDBQuery(parquetPath, query string) ([]string, []map[string]interface{}, QueryStats, error) {
	var stats QueryStats

	if !isSelectOnlySQL(query) {
		return nil, nil, stats, fmt.Errorf("only SELECT statements are permitted")
	}

	t0 := time.Now()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, nil, stats, fmt.Errorf("failed to open DuckDB : %w", err)
	}
	defer db.Close()
	defer appStats.logDuckDBMemory(db)

	safePath := strings.ReplaceAll(parquetPath, "'", "''")
	_, err = db.Exec(fmt.Sprintf("CREATE VIEW virtual_table AS SELECT * FROM read_parquet('%s')", safePath))
	if err != nil {
		return nil, nil, stats, fmt.Errorf("failed to load parquet file: %w", err)
	}
	stats.LoadDuration = time.Since(t0)

	t1 := time.Now()
	rows, err := db.Query(query)
	if err != nil {
		return nil, nil, stats, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, nil, stats, fmt.Errorf("failed to get columns: %w", err)
	}

	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	var resultRows []map[string]interface{}
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, nil, stats, fmt.Errorf("scan failed: %w", err)
		}
		row := make(map[string]interface{})
		for i, col := range columns {
			row[col] = values[i]
		}
		resultRows = append(resultRows, row)
	}
	stats.QueryDuration = time.Since(t1)

	return columns, resultRows, stats, rows.Err()
}

// executeDuckDBJoinQuery opens an in-memory DuckDB instance, registers each parquet file
// as a view named by its alias (e.g. t1, t2), then runs the provided SQL query.
// aliases maps alias name -> parquet file path.
func executeDuckDBJoinQuery(aliases map[string]string, query string) ([]string, []map[string]interface{}, QueryStats, error) {
	var stats QueryStats

	if !isSelectOnlySQL(query) {
		return nil, nil, stats, fmt.Errorf("only SELECT statements are permitted")
	}

	t0 := time.Now()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, nil, stats, fmt.Errorf("failed to open DuckDB : %w", err)
	}
	defer db.Close()
	defer appStats.logDuckDBMemory(db)

	for alias, path := range aliases {
		safePath := strings.ReplaceAll(path, "'", "''")
		if _, err = db.Exec(fmt.Sprintf("CREATE VIEW %s AS SELECT * FROM read_parquet('%s')", alias, safePath)); err != nil {
			return nil, nil, stats, fmt.Errorf("failed to register view %s: %w", alias, err)
		}
	}
	stats.LoadDuration = time.Since(t0)

	t1 := time.Now()
	rows, err := db.Query(query)
	if err != nil {
		return nil, nil, stats, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, nil, stats, fmt.Errorf("failed to get columns: %w", err)
	}

	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	var resultRows []map[string]interface{}
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, nil, stats, fmt.Errorf("scan failed: %w", err)
		}
		row := make(map[string]interface{})
		for i, col := range columns {
			row[col] = values[i]
		}
		resultRows = append(resultRows, row)
	}
	stats.QueryDuration = time.Since(t1)

	return columns, resultRows, stats, rows.Err()
}

// isSelectOnlySQL validates that query is a single SELECT statement with no chained statements.
// It strips comments and string/identifier literals before checking, covering common SQL injection vectors.
func isSelectOnlySQL(query string) bool {
	blockComment := regexp.MustCompile(`(?s)/\*.*?\*/`)
	cleaned := blockComment.ReplaceAllString(query, " ")

	lineComment := regexp.MustCompile(`--[^\n]*`)
	cleaned = lineComment.ReplaceAllString(cleaned, " ")

	stringLit := regexp.MustCompile(`'(?:[^']|'')*'`)
	cleaned = stringLit.ReplaceAllString(cleaned, "''")

	quotedIdent := regexp.MustCompile(`"(?:[^"]|"")*"`)
	cleaned = quotedIdent.ReplaceAllString(cleaned, `""`)

	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return false
	}

	if !regexp.MustCompile(`(?i)^SELECT\b`).MatchString(cleaned) {
		return false
	}

	cleaned = strings.TrimRight(cleaned, "; \t\n\r")
	return !strings.Contains(cleaned, ";")
}

// formatQueryDuration formats a short duration with sub-second precision (µs / ms / s).
func formatQueryDuration(d time.Duration) string {
	switch {
	case d >= time.Second:
		return fmt.Sprintf("%.3fs", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
}

// previewRowLimit is the maximum number of rows fetched for on-screen display when a query
// has no explicit LIMIT. Saves re-execute the full query via streaming to avoid memory pressure.
const previewRowLimit = 200

// hasExplicitLimit reports whether query contains a LIMIT clause (after stripping comments/literals).
func hasExplicitLimit(query string) bool {
	cleaned := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(query, " ")
	cleaned = regexp.MustCompile(`--[^\n]*`).ReplaceAllString(cleaned, " ")
	cleaned = regexp.MustCompile(`'(?:[^']|'')*'`).ReplaceAllString(cleaned, "''")
	cleaned = regexp.MustCompile(`"(?:[^"]|"")*"`).ReplaceAllString(cleaned, `""`)
	return regexp.MustCompile(`(?i)\bLIMIT\b`).MatchString(cleaned)
}

// buildPreviewQuery wraps query in a LIMIT previewRowLimit subquery when the query has no
// explicit LIMIT, enabling fast display without loading the full result set.
// Returns the query to run and whether a preview cap was applied.
func buildPreviewQuery(query string) (string, bool) {
	if hasExplicitLimit(query) {
		return query, false
	}
	trimmed := strings.TrimRight(strings.TrimSpace(query), ";")
	return fmt.Sprintf("SELECT * FROM (%s) _preview LIMIT %d", trimmed, previewRowLimit), true
}

// executeDuckDBQueryPreview runs query against a parquet file, capping results at previewRowLimit
// when no explicit LIMIT is present. Returns isPreview=true when rows were capped.
func executeDuckDBQueryPreview(parquetPath, query string) ([]string, []map[string]interface{}, QueryStats, bool, error) {
	displayQuery, isPreview := buildPreviewQuery(query)
	cols, rows, stats, err := executeDuckDBQuery(parquetPath, displayQuery)
	if isPreview && len(rows) < previewRowLimit {
		isPreview = false
	}
	return cols, rows, stats, isPreview, err
}

// executeDuckDBQueryStreamed runs query against a parquet file and delivers rows in batches
// to avoid loading the full result set into memory. colFn is called once with column names.
// rowFn is called per batch; returning a non-nil error aborts iteration.
// Returns stats and total rows processed.
func executeDuckDBQueryStreamed(parquetPath, query string, batchSize int, colFn func([]string), rowFn func([]map[string]interface{}) error) (QueryStats, int, error) {
	var stats QueryStats

	t0 := time.Now()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return stats, 0, fmt.Errorf("failed to open DuckDB : %w", err)
	}
	defer db.Close()
	defer appStats.logDuckDBMemory(db)

	safePath := strings.ReplaceAll(parquetPath, "'", "''")
	if _, err = db.Exec(fmt.Sprintf("CREATE VIEW virtual_table AS SELECT * FROM read_parquet('%s')", safePath)); err != nil {
		return stats, 0, fmt.Errorf("failed to load parquet file: %w", err)
	}
	stats.LoadDuration = time.Since(t0)

	t1 := time.Now()
	rows, err := db.Query(query)
	if err != nil {
		return stats, 0, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return stats, 0, fmt.Errorf("failed to get columns: %w", err)
	}
	colFn(columns)

	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	var totalRows int
	batch := make([]map[string]interface{}, 0, batchSize)
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return stats, totalRows, fmt.Errorf("scan failed: %w", err)
		}
		row := make(map[string]interface{})
		for i, col := range columns {
			row[col] = values[i]
		}
		batch = append(batch, row)
		if len(batch) >= batchSize {
			if err := rowFn(batch); err != nil {
				return stats, totalRows, err
			}
			totalRows += len(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := rowFn(batch); err != nil {
			return stats, totalRows, err
		}
		totalRows += len(batch)
	}
	stats.QueryDuration = time.Since(t1)

	return stats, totalRows, rows.Err()
}

// executeDuckDBJoinQueryPreview runs a JOIN query capped at previewRowLimit when no explicit
// LIMIT is present. Returns isPreview=true when rows were capped.
func executeDuckDBJoinQueryPreview(aliases map[string]string, query string) ([]string, []map[string]interface{}, QueryStats, bool, error) {
	displayQuery, isPreview := buildPreviewQuery(query)
	cols, rows, stats, err := executeDuckDBJoinQuery(aliases, displayQuery)
	if isPreview && len(rows) < previewRowLimit {
		isPreview = false
	}
	return cols, rows, stats, isPreview, err
}

// executeDuckDBJoinQueryStreamed runs a JOIN query against multiple parquet views and delivers
// rows in batches without loading the full result set into memory.
// colFn is called once with column names. rowFn is called per batch.
func executeDuckDBJoinQueryStreamed(aliases map[string]string, query string, batchSize int, colFn func([]string), rowFn func([]map[string]interface{}) error) (QueryStats, int, error) {
	var stats QueryStats

	t0 := time.Now()
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return stats, 0, fmt.Errorf("failed to open DuckDB : %w", err)
	}
	defer db.Close()
	defer appStats.logDuckDBMemory(db)

	for alias, path := range aliases {
		safePath := strings.ReplaceAll(path, "'", "''")
		if _, err = db.Exec(fmt.Sprintf("CREATE VIEW %s AS SELECT * FROM read_parquet('%s')", alias, safePath)); err != nil {
			return stats, 0, fmt.Errorf("failed to register view %s: %w", alias, err)
		}
	}
	stats.LoadDuration = time.Since(t0)

	t1 := time.Now()
	rows, err := db.Query(query)
	if err != nil {
		return stats, 0, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return stats, 0, fmt.Errorf("failed to get columns: %w", err)
	}
	colFn(columns)

	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	var totalRows int
	batch := make([]map[string]interface{}, 0, batchSize)
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return stats, totalRows, fmt.Errorf("scan failed: %w", err)
		}
		row := make(map[string]interface{})
		for i, col := range columns {
			row[col] = values[i]
		}
		batch = append(batch, row)
		if len(batch) >= batchSize {
			if err := rowFn(batch); err != nil {
				return stats, totalRows, err
			}
			totalRows += len(batch)
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := rowFn(batch); err != nil {
			return stats, totalRows, err
		}
		totalRows += len(batch)
	}
	stats.QueryDuration = time.Since(t1)

	return stats, totalRows, rows.Err()
}
