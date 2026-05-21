package main

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/gocql/gocql"
)

// fetchKeyspaces retrieves all keyspaces (both user and system)
func fetchKeyspaces(session *gocql.Session) ([]BrowseKeyspace, error) {
	var keyspaces []BrowseKeyspace

	// Query system_schema.keyspaces with idempotent flag for automatic retries
	iter := session.Query("SELECT keyspace_name FROM system_schema.keyspaces").
		Idempotent(true).
		Iter()
	var keyspaceName string
	for iter.Scan(&keyspaceName) {
		keyspaces = append(keyspaces, BrowseKeyspace{Name: keyspaceName})
	}

	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to fetch keyspaces: %w", err)
	}

	// Sort keyspaces alphabetically
	sort.Slice(keyspaces, func(i, j int) bool {
		return keyspaces[i].Name < keyspaces[j].Name
	})

	return keyspaces, nil
}

// fetchTables retrieves all tables in a keyspace
func fetchTables(session *gocql.Session, keyspace string) ([]BrowseTable, error) {
	var tables []BrowseTable

	// Query system_schema.tables with parameterized keyspace and idempotent flag
	iter := session.Query(
		"SELECT table_name FROM system_schema.tables WHERE keyspace_name = ?",
		keyspace,
	).Idempotent(true).Iter()

	var tableName string
	for iter.Scan(&tableName) {
		tables = append(tables, BrowseTable{Name: tableName})
	}

	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to fetch tables for keyspace %s: %w", keyspace, err)
	}

	// Sort tables alphabetically
	sort.Slice(tables, func(i, j int) bool {
		return tables[i].Name < tables[j].Name
	})

	return tables, nil
}

// fetchColumns retrieves all columns in a table with key information
func fetchColumns(session *gocql.Session, keyspace, table string) ([]BrowseColumn, error) {
	var columns []BrowseColumn

	// Query system_schema.columns with parameterized values and idempotent flag
	iter := session.Query(
		"SELECT column_name, type, kind, position, clustering_order FROM system_schema.columns WHERE keyspace_name = ? AND table_name = ?",
		keyspace,
		table,
	).Idempotent(true).Iter()

	var columnName, columnType, kind, clusteringOrder string
	var position int
	for iter.Scan(&columnName, &columnType, &kind, &position, &clusteringOrder) {
		columns = append(columns, BrowseColumn{
			Name:            columnName,
			Type:            columnType,
			Kind:            kind,
			Position:        position,
			ClusteringOrder: clusteringOrder,
		})
	}

	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to fetch columns for %s.%s: %w", keyspace, table, err)
	}

	// Sort columns: partition_key first (by position), then clustering (by position), then regular/static
	sort.Slice(columns, func(i, j int) bool {
		kindOrder := map[string]int{"partition_key": 0, "clustering": 1, "regular": 2, "static": 3}
		if kindOrder[columns[i].Kind] != kindOrder[columns[j].Kind] {
			return kindOrder[columns[i].Kind] < kindOrder[columns[j].Kind]
		}
		if columns[i].Kind == "partition_key" || columns[i].Kind == "clustering" {
			return columns[i].Position < columns[j].Position
		}
		return columns[i].Name < columns[j].Name
	})

	return columns, nil
}

// fetchClusterInfoWithSession retrieves cluster information using existing session
func fetchClusterInfoWithSession(session *gocql.Session) (map[string]string, error) {
	info := make(map[string]string)

	// Query system.local for local node info with idempotent flag
	var clusterName, dataCenter, rack, releaseVersion string
	var localIP net.IP
	err := session.Query("SELECT cluster_name, release_version, data_center, rack, rpc_address FROM system.local").
		Idempotent(true).
		Scan(&clusterName, &releaseVersion, &dataCenter, &rack, &localIP)
	if err != nil {
		return nil, fmt.Errorf("failed to query system.local: %w", err)
	}

	info["cluster_name"] = clusterName
	info["release_version"] = releaseVersion
	info["data_center"] = dataCenter
	info["rack"] = rack
	if len(localIP) > 0 {
		info["local_node_ip"] = localIP.String()
	} else {
		info["local_node_ip"] = "-"
	}

	// Count keyspaces
	var keyspaceCount int
	iter := session.Query("SELECT COUNT(1) FROM system_schema.keyspaces").Idempotent(true).Iter()
	iter.Scan(&keyspaceCount)
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to count keyspaces: %w", err)
	}
	info["total_keyspaces"] = fmt.Sprintf("%d", keyspaceCount)

	// Count user keyspaces
	var userKeyspaceCount int
	iter2 := session.Query("SELECT keyspace_name FROM system_schema.keyspaces").Idempotent(true).Iter()
	var ksName string
	for iter2.Scan(&ksName) {
		if ksName != "system" && ksName != "system_auth" && ksName != "system_distributed" && ksName != "system_schema" && ksName != "system_traces" && ksName != "system_virtual_schema" {
			userKeyspaceCount++
		}
	}
	if err := iter2.Close(); err != nil {
		return nil, fmt.Errorf("failed to list keyspaces: %w", err)
	}
	info["user_keyspaces"] = fmt.Sprintf("%d", userKeyspaceCount)

	// Get node count and collect peer IPs
	var peerIPs []string
	peerIter := session.Query("SELECT peer FROM system.peers").Idempotent(true).Iter()
	var peerIP net.IP
	for peerIter.Scan(&peerIP) {
		peerIPs = append(peerIPs, peerIP.String())
	}
	if err := peerIter.Close(); err != nil {
		return nil, fmt.Errorf("failed to list peers: %w", err)
	}
	sort.Strings(peerIPs)
	info["node_count"] = fmt.Sprintf("%d", 1+len(peerIPs))
	if len(peerIPs) == 0 {
		info["peer_nodes"] = "None"
	} else {
		info["peer_nodes"] = strings.Join(peerIPs, ", ")
	}

	return info, nil
}

// fetchKeyspaceInfo retrieves keyspace information
func fetchKeyspaceInfo(session *gocql.Session, keyspace string) (map[string]interface{}, error) {
	info := make(map[string]interface{})
	info["name"] = keyspace

	// Get keyspace details from system_schema.keyspaces with idempotent flag
	var replication map[string]string
	var durableWrites bool
	err := session.Query(
		"SELECT replication, durable_writes FROM system_schema.keyspaces WHERE keyspace_name = ?",
		keyspace,
	).Idempotent(true).Scan(&replication, &durableWrites)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch keyspace info for %s: %w", keyspace, err)
	}

	info["replication"] = replication
	info["durable_writes"] = durableWrites

	// Count tables
	var tableCount int
	iter := session.Query(
		"SELECT COUNT(1) FROM system_schema.tables WHERE keyspace_name = ?",
		keyspace,
	).Idempotent(true).Iter()
	iter.Scan(&tableCount)
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("failed to count tables: %w", err)
	}
	info["table_count"] = tableCount

	// Count user-defined types
	var udtCount int
	iter2 := session.Query(
		"SELECT COUNT(1) FROM system_schema.types WHERE keyspace_name = ?",
		keyspace,
	).Idempotent(true).Iter()
	iter2.Scan(&udtCount)
	if err := iter2.Close(); err != nil {
		return nil, fmt.Errorf("failed to count UDTs: %w", err)
	}
	info["udt_count"] = udtCount

	// Count indexes
	var indexCount int
	iter3 := session.Query(
		"SELECT COUNT(1) FROM system_schema.indexes WHERE keyspace_name = ?",
		keyspace,
	).Idempotent(true).Iter()
	iter3.Scan(&indexCount)
	if err := iter3.Close(); err != nil {
		return nil, fmt.Errorf("failed to count indexes: %w", err)
	}
	info["index_count"] = indexCount

	// Count materialized views
	var mvCount int
	iter4 := session.Query(
		"SELECT COUNT(1) FROM system_schema.views WHERE keyspace_name = ?",
		keyspace,
	).Idempotent(true).Iter()
	iter4.Scan(&mvCount)
	if err := iter4.Close(); err != nil {
		return nil, fmt.Errorf("failed to count views: %w", err)
	}
	info["mv_count"] = mvCount

	return info, nil
}

// fetchTableDDL constructs a CREATE TABLE DDL statement from system schema tables
func fetchTableDDL(session *gocql.Session, keyspace, table string) (string, error) {
	// Fetch table properties
	var bloomFilterFpChance, crcCheckChance, dcLocalReadRepairChance, readRepairChance float64
	var gcGraceSeconds, defaultTTL, maxIndexInterval, memtableFlushPeriod, minIndexInterval int
	var comment, speculativeRetry string
	var caching, compaction, compression map[string]string

	err := session.Query(
		`SELECT bloom_filter_fp_chance, caching, comment, compaction, compression,
		        crc_check_chance, dclocal_read_repair_chance, default_time_to_live,
		        gc_grace_seconds, max_index_interval, memtable_flush_period_in_ms,
		        min_index_interval, read_repair_chance, speculative_retry
		 FROM system_schema.tables WHERE keyspace_name = ? AND table_name = ?`,
		keyspace, table,
	).Idempotent(true).Scan(
		&bloomFilterFpChance, &caching, &comment, &compaction, &compression,
		&crcCheckChance, &dcLocalReadRepairChance, &defaultTTL,
		&gcGraceSeconds, &maxIndexInterval, &memtableFlushPeriod,
		&minIndexInterval, &readRepairChance, &speculativeRetry,
	)
	if err != nil {
		return "", fmt.Errorf("failed to fetch table properties for %s.%s: %w", keyspace, table, err)
	}

	// Fetch columns
	columns, err := fetchColumns(session, keyspace, table)
	if err != nil {
		return "", err
	}

	// Build column definitions
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("CREATE TABLE %s.%s (\n", keyspace, table))

	for _, col := range columns {
		switch col.Kind {
		case "static":
			sb.WriteString(fmt.Sprintf("    %s %s STATIC,\n", col.Name, col.Type))
		default:
			sb.WriteString(fmt.Sprintf("    %s %s,\n", col.Name, col.Type))
		}
	}

	// Build PRIMARY KEY clause
	var partitionKeys, clusteringKeys []BrowseColumn
	for _, col := range columns {
		switch col.Kind {
		case "partition_key":
			partitionKeys = append(partitionKeys, col)
		case "clustering":
			clusteringKeys = append(clusteringKeys, col)
		}
	}

	pkParts := make([]string, len(partitionKeys))
	for i, col := range partitionKeys {
		pkParts[i] = col.Name
	}
	var pkClause string
	if len(partitionKeys) > 1 {
		pkClause = fmt.Sprintf("(%s)", strings.Join(pkParts, ", "))
	} else if len(partitionKeys) == 1 {
		pkClause = partitionKeys[0].Name
	}

	if len(clusteringKeys) > 0 {
		ckParts := make([]string, len(clusteringKeys))
		for i, col := range clusteringKeys {
			ckParts[i] = col.Name
		}
		sb.WriteString(fmt.Sprintf("    PRIMARY KEY (%s, %s)\n", pkClause, strings.Join(ckParts, ", ")))
	} else {
		sb.WriteString(fmt.Sprintf("    PRIMARY KEY (%s)\n", pkClause))
	}
	sb.WriteString(")")

	// WITH options
	withParts := []string{}

	// CLUSTERING ORDER BY
	if len(clusteringKeys) > 0 {
		orderParts := make([]string, len(clusteringKeys))
		for i, col := range clusteringKeys {
			order := col.ClusteringOrder
			if order == "" {
				order = "ASC"
			}
			orderParts[i] = fmt.Sprintf("%s %s", col.Name, order)
		}
		withParts = append(withParts, fmt.Sprintf("CLUSTERING ORDER BY (%s)", strings.Join(orderParts, ", ")))
	}

	withParts = append(withParts, fmt.Sprintf("bloom_filter_fp_chance = %.4g", bloomFilterFpChance))

	if len(caching) > 0 {
		withParts = append(withParts, fmt.Sprintf("caching = %s", formatMapOption(caching)))
	}

	if comment != "" {
		withParts = append(withParts, fmt.Sprintf("comment = '%s'", strings.ReplaceAll(comment, "'", "''")))
	} else {
		withParts = append(withParts, "comment = ''")
	}

	if len(compaction) > 0 {
		withParts = append(withParts, fmt.Sprintf("compaction = %s", formatMapOption(compaction)))
	}

	if len(compression) > 0 {
		withParts = append(withParts, fmt.Sprintf("compression = %s", formatMapOption(compression)))
	}

	withParts = append(withParts,
		fmt.Sprintf("crc_check_chance = %.4g", crcCheckChance),
		fmt.Sprintf("dclocal_read_repair_chance = %.4g", dcLocalReadRepairChance),
		fmt.Sprintf("default_time_to_live = %d", defaultTTL),
		fmt.Sprintf("gc_grace_seconds = %d", gcGraceSeconds),
		fmt.Sprintf("max_index_interval = %d", maxIndexInterval),
		fmt.Sprintf("memtable_flush_period_in_ms = %d", memtableFlushPeriod),
		fmt.Sprintf("min_index_interval = %d", minIndexInterval),
		fmt.Sprintf("read_repair_chance = %.4g", readRepairChance),
		fmt.Sprintf("speculative_retry = '%s'", speculativeRetry),
	)

	sb.WriteString("\n    WITH ")
	sb.WriteString(strings.Join(withParts, "\n    AND "))
	sb.WriteString(";")

	return sb.String(), nil
}

// formatMapOption formats a map as a CQL map literal
func formatMapOption(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	pairs := make([]string, 0, len(m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("'%s': '%s'", k, m[k]))
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

// fetchTableData retrieves data from a table (limited to first 20 rows)
func fetchTableData(session *gocql.Session, keyspace, table string) ([]string, []map[string]interface{}, error) {
	// Query table data with limit using quoted identifiers for safety
	// Quoted identifiers protect against special characters in keyspace/table names
	query := fmt.Sprintf(`SELECT * FROM "%s"."%s" LIMIT 20`, keyspace, table)
	iter := session.Query(query).Idempotent(true).Iter()

	// Get column names from the query metadata
	columns := iter.Columns()
	var columnNames []string
	for _, col := range columns {
		columnNames = append(columnNames, col.Name)
	}

	// Fetch all rows
	var rows []map[string]interface{}
	for {
		row := make(map[string]interface{})
		if !iter.MapScan(row) {
			break
		}
		rows = append(rows, row)
	}

	if err := iter.Close(); err != nil {
		return nil, nil, fmt.Errorf("failed to fetch data from %s.%s: %w", keyspace, table, err)
	}

	return columnNames, rows, nil
}
