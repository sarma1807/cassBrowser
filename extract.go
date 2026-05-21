package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
	inf "gopkg.in/inf.v0"
)

type ExtractResult struct {
	RowCount  int
	FilePath  string
	Duration  time.Duration
	Cancelled bool
}

// extractTableToParquet extracts rows from a Cassandra table into a Parquet file.
// selectedColumns limits which columns are extracted; nil or empty means all columns.
// whereClause is an optional CQL WHERE clause (without the keyword); empty means no filter.
// progressFn is called periodically with a status string for UI updates.
// Cancelling ctx stops the row loop and finalizes the partial file; result.Cancelled is set true.
func extractTableToParquet(
	ctx context.Context,
	conn DecryptedConnection,
	keyspace, table string,
	selectedColumns []string,
	whereClause string,
	outputDir, filename string,
	progressFn func(string),
) (ExtractResult, error) {
	start := time.Now()

	session, err := createBrowseSession(conn)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("failed to connect : %w", err)
	}
	defer closeBrowseSession(session)

	progressFn("Fetching column metadata ...")
	columns, err := fetchColumns(session, keyspace, table)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("failed to fetch column metadata : %w", err)
	}
	if len(columns) == 0 {
		return ExtractResult{}, fmt.Errorf("no columns found for %s.%s", keyspace, table)
	}

	// Build the set of columns to extract (all if none specified).
	var extractCols []BrowseColumn
	if len(selectedColumns) > 0 {
		selectedSet := make(map[string]bool, len(selectedColumns))
		for _, c := range selectedColumns {
			selectedSet[c] = true
		}
		for _, col := range columns {
			if selectedSet[col.Name] {
				extractCols = append(extractCols, col)
			}
		}
	} else {
		extractCols = columns
	}

	// Sort alphabetically — parquet-go sorts Group keys the same way,
	// so our row slice positions will match the schema leaf column order.
	sortedCols := make([]BrowseColumn, len(extractCols))
	copy(sortedCols, extractCols)
	sort.Slice(sortedCols, func(i, j int) bool {
		return sortedCols[i].Name < sortedCols[j].Name
	})

	// Choose batch size based on column types: wide columns (blob, text, collections)
	// get a smaller page to bound memory; narrow columns get a larger page for throughput.
	wideCount := 0
	for _, col := range sortedCols {
		switch normalizeCassBaseType(col.Type) {
		case "blob", "text", "varchar", "ascii", "list", "set", "map", "tuple":
			wideCount++
		}
	}
	pageSize := 5000
	if wideCount > len(sortedCols)/3 {
		pageSize = 2000
	}

	// Build parquet schema; all columns are optional to handle Cassandra nulls.
	group := parquet.Group{}
	for _, col := range sortedCols {
		group[col.Name] = parquet.Optional(cassTypeToParquetNode(col.Type))
	}
	schema := parquet.NewSchema("row", group)

	filePath := filepath.Join(outputDir, filename)
	f, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return ExtractResult{}, fmt.Errorf("failed to create output file : %w", err)
	}
	defer f.Close()

	writerOpts := []parquet.WriterOption{schema, parquet.MaxRowsPerRowGroup(int64(pageSize))}
	for _, col := range sortedCols {
		writerOpts = append(writerOpts, parquet.KeyValueMetadata("cassandra.column."+col.Name+".type", cassTypeMetaValue(col.Type, col.Name)))
	}
	writer := parquet.NewWriter(f, writerOpts...)

	colList := "*"
	if len(selectedColumns) > 0 {
		names := make([]string, len(sortedCols))
		for i, col := range sortedCols {
			names[i] = col.Name
		}
		colList = strings.Join(names, ", ")
	}
	query := fmt.Sprintf("SELECT %s FROM %s.%s", colList, keyspace, table)
	if wc := strings.TrimSpace(whereClause); wc != "" {
		prefix := strings.ToUpper(wc)
		if !strings.HasPrefix(prefix, "WHERE ") && !strings.HasPrefix(prefix, "WHERE\t") {
			wc = "WHERE " + wc
		}
		query += " " + wc + " ALLOW FILTERING"
	}

	progressFn("Querying Cassandra ...")
	iter := session.Query(query).WithContext(ctx).PageSize(pageSize).Iter()

	rowCount := 0
	dataRow := make(map[string]interface{}, len(sortedCols))
	parquetRow := make(parquet.Row, len(sortedCols))
	rowBuf := []parquet.Row{parquetRow}
	for {
		if ctx.Err() != nil {
			iter.Close()
			writer.Close()
			return ExtractResult{
				RowCount:  rowCount,
				FilePath:  filePath,
				Duration:  time.Since(start),
				Cancelled: true,
			}, nil
		}

		clear(dataRow)
		if !iter.MapScan(dataRow) {
			break
		}

		// Build parquet.Row in alphabetical column order (matches schema leaf order).
		for i, col := range sortedCols {
			parquetRow[i] = toParquetValue(dataRow[col.Name], col.Type, i)
		}

		if _, err := writer.WriteRows(rowBuf); err != nil {
			iter.Close()
			writer.Close()
			os.Remove(filePath)
			return ExtractResult{}, fmt.Errorf("failed to write row %d: %w", rowCount+1, err)
		}

		rowCount++
		if rowCount%5000 == 0 {
			progressFn(fmt.Sprintf("Extracted %s rows ...", formatRowCount(int64(rowCount))))
		}
	}

	if err := iter.Close(); err != nil {
		writer.Close()
		if ctx.Err() != nil {
			// Context was cancelled while MapScan was blocking — treat as clean cancellation.
			// Partial rows already written; writer was closed above to finalize the file.
			return ExtractResult{
				RowCount:  rowCount,
				FilePath:  filePath,
				Duration:  time.Since(start),
				Cancelled: true,
			}, nil
		}
		os.Remove(filePath)
		return ExtractResult{}, fmt.Errorf("query error : %w", err)
	}

	if err := writer.Close(); err != nil {
		os.Remove(filePath)
		return ExtractResult{}, fmt.Errorf("failed to finalize parquet file : %w", err)
	}

	duration := time.Since(start)
	return ExtractResult{
		RowCount: rowCount,
		FilePath: filePath,
		Duration: duration,
	}, nil
}

// duckdbToCQLValue converts a value returned by the DuckDB Go driver to a type
// compatible with gocql query parameter binding.
// DuckDB v2 returns VARCHAR as string, timestamps/dates as time.Time, numerics as
// int32/int64/float32/float64. The only defensive case needed is []byte → string
// for text-like types on older drivers.
func duckdbToCQLValue(val interface{}, cassType string) interface{} {
	if val == nil {
		return nil
	}
	switch normalizeCassBaseType(cassType) {
	case "text", "varchar", "ascii", "uuid", "timeuuid", "inet":
		if v, ok := val.([]byte); ok {
			return string(v)
		}
	case "varint":
		s, _ := val.(string)
		if s == "" {
			if b, ok := val.([]byte); ok {
				s = string(b)
			}
		}
		n := new(big.Int)
		n.SetString(s, 10)
		return n
	case "decimal":
		s, _ := val.(string)
		if s == "" {
			if b, ok := val.([]byte); ok {
				s = string(b)
			}
		}
		d := new(inf.Dec)
		d.SetString(s)
		return d
	}
	return val
}

// cassTypeToParquetNode maps a Cassandra type string to a parquet-go Node.
func cassTypeToParquetNode(cassType string) parquet.Node {
	switch normalizeCassBaseType(cassType) {
	case "boolean":
		return parquet.Leaf(parquet.BooleanType)
	case "tinyint", "smallint", "int":
		return parquet.Int(32)
	case "bigint", "counter":
		return parquet.Int(64)
	case "varint":
		return parquet.String() // *big.Int stored as exact decimal string
	case "float":
		return parquet.Leaf(parquet.FloatType)
	case "double":
		return parquet.Leaf(parquet.DoubleType)
	case "decimal":
		return parquet.String() // *inf.Dec stored as exact decimal string
	case "timestamp":
		return parquet.Timestamp(parquet.Millisecond)
	case "time":
		return parquet.Time(parquet.Nanosecond)
	case "date":
		return parquet.Date() // physical Int32, days since Unix epoch
	case "blob":
		return parquet.Leaf(parquet.ByteArrayType)
	case "list", "set", "map", "tuple":
		return parquet.JSON()
	default: // text, varchar, ascii, uuid, timeuuid, inet, …
		return parquet.String()
	}
}

// toParquetValue converts a gocql-scanned value to a parquet.Value for an optional column.
// colIdx is the 0-based leaf column index in the schema (alphabetical order).
func toParquetValue(val interface{}, cassType string, colIdx int) parquet.Value {
	if val == nil {
		// definition level 0 = null for an optional field
		return parquet.NullValue().Level(0, 0, colIdx)
	}

	// definition level 1 = value present for an optional field
	const def = 1

	switch normalizeCassBaseType(cassType) {
	case "boolean":
		if v, ok := val.(bool); ok {
			return parquet.BooleanValue(v).Level(0, def, colIdx)
		}
	case "tinyint":
		switch v := val.(type) {
		case int8:
			return parquet.Int32Value(int32(v)).Level(0, def, colIdx)
		case int:
			return parquet.Int32Value(int32(v)).Level(0, def, colIdx)
		}
	case "smallint":
		switch v := val.(type) {
		case int16:
			return parquet.Int32Value(int32(v)).Level(0, def, colIdx)
		case int:
			return parquet.Int32Value(int32(v)).Level(0, def, colIdx)
		}
	case "int":
		switch v := val.(type) {
		case int:
			return parquet.Int32Value(int32(v)).Level(0, def, colIdx)
		case int32:
			return parquet.Int32Value(v).Level(0, def, colIdx)
		}
	case "bigint", "counter":
		switch v := val.(type) {
		case int64:
			return parquet.Int64Value(v).Level(0, def, colIdx)
		case int:
			return parquet.Int64Value(int64(v)).Level(0, def, colIdx)
		}
	case "varint":
		if v, ok := val.(*big.Int); ok {
			return parquet.ByteArrayValue([]byte(v.String())).Level(0, def, colIdx)
		}
	case "float":
		if v, ok := val.(float32); ok {
			return parquet.FloatValue(v).Level(0, def, colIdx)
		}
	case "double":
		if v, ok := val.(float64); ok {
			return parquet.DoubleValue(v).Level(0, def, colIdx)
		}
	case "decimal":
		if v, ok := val.(*inf.Dec); ok {
			return parquet.ByteArrayValue([]byte(v.String())).Level(0, def, colIdx)
		}
	case "timestamp":
		if t, ok := val.(time.Time); ok {
			return parquet.Int64Value(t.UnixMilli()).Level(0, def, colIdx)
		}
	case "time":
		if v, ok := val.(int64); ok {
			return parquet.Int64Value(v).Level(0, def, colIdx)
		}
	case "date":
		if t, ok := val.(time.Time); ok {
			// gocql returns midnight UTC; Unix() is exactly divisible by 86400
			return parquet.Int32Value(int32(t.Unix()/86400)).Level(0, def, colIdx)
		}
	case "blob":
		if v, ok := val.([]byte); ok {
			return parquet.ByteArrayValue(v).Level(0, def, colIdx)
		}
	case "list", "set", "map", "tuple":
		if b, err := json.Marshal(val); err == nil {
			return parquet.ByteArrayValue(b).Level(0, def, colIdx)
		}
	}

	// Fallback: string representation (uuid, timeuuid, inet, text, varchar, ascii, and unknowns)
	return parquet.ByteArrayValue([]byte(fmt.Sprintf("%v", val))).Level(0, def, colIdx)
}

// cassTypeMetaValue builds the parquet key-value metadata value for a column.
// For types stored as STRING or JSON (requiring an explicit DuckDB cast to recover
// the original semantics), it appends a ready-to-use cast expression.
// For natively-mapped types no cast hint is needed.
func cassTypeMetaValue(cassType, colName string) string {
	var cast string
	switch normalizeCassBaseType(cassType) {
	case "varint":
		cast = fmt.Sprintf("TRY_CAST(%s AS HUGEINT)", colName)
	case "decimal":
		cast = fmt.Sprintf("TRY_CAST(%s AS DECIMAL)", colName)
	case "uuid", "timeuuid":
		cast = fmt.Sprintf("%s::UUID", colName)
	case "list", "set", "map", "tuple":
		cast = fmt.Sprintf("%s::JSON", colName)
	}
	if cast != "" {
		return cassType + " : " + cast
	}
	return cassType
}

// normalizeCassBaseType strips parameters and lowercases a Cassandra type, e.g.
// "list<text>" → "list", "BIGINT" → "bigint", "frozen<list<text>>" → "list".
func normalizeCassBaseType(cassType string) string {
	s := strings.ToLower(strings.TrimSpace(cassType))
	if i := strings.Index(s, "<"); i != -1 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if s == "frozen" {
		orig := strings.ToLower(strings.TrimSpace(cassType))
		start := strings.Index(orig, "<")
		end := strings.LastIndex(orig, ">")
		if start != -1 && end > start {
			return normalizeCassBaseType(orig[start+1 : end])
		}
	}
	return s
}

// ParquetColumnInfo holds a column name, its parquet type string, and optional Cassandra source type.
type ParquetColumnInfo struct {
	Name          string
	Type          string
	CassandraType string // populated from cassandra.column.<name>.type file metadata, empty if absent
}

// ParquetPreview holds a file's schema and a sample of its rows.
type ParquetPreview struct {
	Columns   []ParquetColumnInfo
	Rows      [][]string
	TotalRows int64
	FileSize  int64
	ModTime   time.Time
}

// readParquetPreview opens a parquet file and returns schema + up to maxRows rows.
func readParquetPreview(path string, maxRows int) (ParquetPreview, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return ParquetPreview{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return ParquetPreview{}, err
	}
	defer f.Close()

	pf, err := parquet.OpenFile(f, fi.Size())
	if err != nil {
		return ParquetPreview{}, err
	}

	schema := pf.Schema()
	fields := schema.Fields()
	cols := make([]ParquetColumnInfo, len(fields))
	for i, field := range fields {
		cassType, _ := pf.Lookup("cassandra.column." + field.Name() + ".type")
		cols[i] = ParquetColumnInfo{
			Name:          field.Name(),
			Type:          field.Type().String(),
			CassandraType: cassType,
		}
	}

	totalRows := pf.NumRows()
	nCols := len(cols)

	rows := make([][]string, 0, maxRows)
	buf := make([]parquet.Row, 100)
outer:
	for _, rg := range pf.RowGroups() {
		reader := rg.Rows()
		for {
			n, readErr := reader.ReadRows(buf)
			for i := 0; i < n; i++ {
				vals := make([]string, nCols)
				for j, v := range buf[i] {
					if j >= nCols {
						break
					}
					ft := cols[j].Type
					vals[j] = parquetValueToString(v, ft)
				}
				rows = append(rows, vals)
				if len(rows) >= maxRows {
					reader.Close()
					break outer
				}
			}
			if readErr != nil {
				break
			}
		}
		reader.Close()
	}

	return ParquetPreview{
		Columns:   cols,
		Rows:      rows,
		TotalRows: totalRows,
		FileSize:  fi.Size(),
		ModTime:   fi.ModTime(),
	}, nil
}

// parquetValueToString converts a parquet.Value to its string representation.
// fieldType is the parquet type string (e.g. "DATE", "TIMESTAMP(...)") used to
// apply logical-type-aware formatting for date/time columns.
func parquetValueToString(v parquet.Value, fieldType string) string {
	if v.IsNull() {
		return ""
	}
	switch v.Kind() {
	case parquet.Boolean:
		if v.Boolean() {
			return "true"
		}
		return "false"
	case parquet.Int32:
		if strings.Contains(fieldType, "DATE") {
			t := time.Unix(int64(v.Int32())*86400, 0).UTC()
			return t.Format("2006-01-02")
		}
		return fmt.Sprintf("%d", v.Int32())
	case parquet.Int64:
		if strings.HasPrefix(fieldType, "TIMESTAMP") {
			return time.UnixMilli(v.Int64()).UTC().Format("2006-01-02 15:04:05.000 UTC")
		}
		if strings.HasPrefix(fieldType, "TIME") {
			ns := v.Int64()
			h := ns / 3_600_000_000_000
			ns %= 3_600_000_000_000
			m := ns / 60_000_000_000
			ns %= 60_000_000_000
			s := ns / 1_000_000_000
			ns %= 1_000_000_000
			return fmt.Sprintf("%02d:%02d:%02d.%09d", h, m, s, ns)
		}
		return fmt.Sprintf("%d", v.Int64())
	case parquet.Float:
		val := float64(v.Float())
		if val == 0 {
			return "0"
		}
		if math.Abs(val) >= 1e9 {
			return strconv.FormatFloat(val, 'g', 7, 32)
		}
		s := strconv.FormatFloat(val, 'f', 6, 32)
		s = strings.TrimRight(s, "0")
		return strings.TrimRight(s, ".")
	case parquet.Double:
		val := v.Double()
		if val == 0 {
			return "0"
		}
		if math.Abs(val) >= 1e15 {
			return strconv.FormatFloat(val, 'g', 15, 64)
		}
		s := strconv.FormatFloat(val, 'f', 10, 64)
		s = strings.TrimRight(s, "0")
		return strings.TrimRight(s, ".")
	case parquet.ByteArray, parquet.FixedLenByteArray:
		return string(v.ByteArray())
	default:
		return fmt.Sprintf("%v", v)
	}
}
