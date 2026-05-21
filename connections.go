package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

var (
	reSelectFrom    = regexp.MustCompile(`(?i)FROM\s+(?:"([^"]+)"|([a-zA-Z_][a-zA-Z0-9_]*))\s*\.\s*(?:"([^"]+)"|([a-zA-Z_][a-zA-Z0-9_]*))|FROM\s+(?:"([^"]+)"|([a-zA-Z_][a-zA-Z0-9_]*))`)
	reLimitClause   = regexp.MustCompile(`\bLIMIT\s+\d+`)
	reLimitValue    = regexp.MustCompile(`(?i)\bLIMIT\s+(\d+)`)
	reBlockComment  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reLineComment   = regexp.MustCompile(`--[^\n]*`)
	reStringLiteral = regexp.MustCompile(`'(?:[^']|'')*'`)
	reSelectStart   = regexp.MustCompile(`(?i)^SELECT\b`)
)

// Connection represents a stored connection (may be encrypted)
type Connection struct {
	ConnName       string `json:"conn_name"`
	EncryptedData  string `json:"encrypted_data,omitempty"`  // Used when EncryptConnectionDetails=yes
	Environment    string `json:"environment,omitempty"`     // Used when EncryptConnectionDetails=no
	IPAddresses    string `json:"ip_addresses,omitempty"`    // Used when EncryptConnectionDetails=no
	Port           string `json:"port,omitempty"`            // Used when EncryptConnectionDetails=no
	Username       string `json:"username,omitempty"`        // Used when EncryptConnectionDetails=no
	Password       string `json:"password,omitempty"`        // Used when EncryptConnectionDetails=no
	PromptUsername bool   `json:"prompt_username,omitempty"` // Used when EncryptConnectionDetails=no
	PromptPassword bool   `json:"prompt_password,omitempty"` // Used when EncryptConnectionDetails=no
}

// DecryptedConnection holds the decrypted connection details for internal use
type DecryptedConnection struct {
	ConnName       string
	Environment    string
	IPAddresses    string
	Port           string
	Username       string
	Password       string
	PromptUsername bool
	PromptPassword bool
}

// BrowseKeyspace represents a Cassandra keyspace for browsing
type BrowseKeyspace struct {
	Name string
}

// BrowseTable represents a Cassandra table for browsing
type BrowseTable struct {
	Name string
}

// BrowseColumn represents a Cassandra column for browsing
type BrowseColumn struct {
	Name            string
	Type            string
	Kind            string // partition_key, clustering, regular, static
	Position        int    // position within partition key or clustering key
	ClusteringOrder string // ASC or DESC (clustering columns only)
}

var connectionsFile string

func initConnectionsPaths() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory if home dir not found
		connectionsFile = filepath.Join("."+AppName, AppName+".connections")
	} else {
		connectionsFile = filepath.Join(homeDir, "."+AppName, AppName+".connections")
	}
}

func init() {
	initConnectionsPaths()
}

func loadConnections() []Connection {
	var connections []Connection
	data, err := os.ReadFile(connectionsFile)
	if err != nil {
		return connections
	}
	json.Unmarshal(data, &connections)
	return connections
}

func saveConnection(decConn DecryptedConnection) error {
	// Ensure the directory exists
	dir := filepath.Dir(connectionsFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	var conn Connection

	// Check if encryption is enabled
	if EncryptConnectionDetails == "yes" {
		// Pack and encrypt the connection data
		packedData := packConnectionData(
			decConn.Environment,
			decConn.IPAddresses,
			decConn.Port,
			decConn.Username,
			decConn.Password,
			decConn.PromptUsername,
			decConn.PromptPassword,
		)

		encryptedData, err := encryptConnectionData(packedData)
		if err != nil {
			return err
		}

		// Create the connection with encrypted data
		conn = Connection{
			ConnName:      decConn.ConnName,
			EncryptedData: encryptedData,
		}
	} else {
		// Store in plain text format
		conn = Connection{
			ConnName:       decConn.ConnName,
			Environment:    decConn.Environment,
			IPAddresses:    decConn.IPAddresses,
			Port:           decConn.Port,
			Username:       decConn.Username,
			Password:       decConn.Password,
			PromptUsername: decConn.PromptUsername,
			PromptPassword: decConn.PromptPassword,
		}
	}

	connections := loadConnections()
	connections = append(connections, conn)
	data, err := json.MarshalIndent(connections, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(connectionsFile, append(data, '\n'), 0600)
}

// decryptConnection decrypts a Connection into a DecryptedConnection
func decryptConnection(conn Connection) (DecryptedConnection, error) {
	// Check if data is encrypted
	if conn.EncryptedData != "" {
		// Decrypt the encrypted data
		decryptedData, err := decryptConnectionData(conn.EncryptedData)
		if err != nil {
			return DecryptedConnection{}, err
		}

		env, ips, port, user, pass, promptUser, promptPass, err := unpackConnectionData(decryptedData)
		if err != nil {
			return DecryptedConnection{}, err
		}

		return DecryptedConnection{
			ConnName:       conn.ConnName,
			Environment:    env,
			IPAddresses:    ips,
			Port:           port,
			Username:       user,
			Password:       pass,
			PromptUsername: promptUser,
			PromptPassword: promptPass,
		}, nil
	} else {
		// Data is in plain text format
		return DecryptedConnection{
			ConnName:       conn.ConnName,
			Environment:    conn.Environment,
			IPAddresses:    conn.IPAddresses,
			Port:           conn.Port,
			Username:       conn.Username,
			Password:       conn.Password,
			PromptUsername: conn.PromptUsername,
			PromptPassword: conn.PromptPassword,
		}, nil
	}
}

// loadDecryptedConnections loads and decrypts all connections
func loadDecryptedConnections() ([]DecryptedConnection, error) {
	connections := loadConnections()
	decrypted := make([]DecryptedConnection, 0, len(connections))

	for _, conn := range connections {
		decConn, err := decryptConnection(conn)
		if err != nil {
			// Skip connections that fail to decrypt
			continue
		}
		decrypted = append(decrypted, decConn)
	}

	return decrypted, nil
}

// deleteConnection removes a connection by name
func deleteConnection(connName string) error {
	connections := loadConnections()

	// Find and remove the connection
	newConnections := []Connection{}
	found := false
	for _, conn := range connections {
		if conn.ConnName != connName {
			newConnections = append(newConnections, conn)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("connection not found: %s", connName)
	}

	// Save updated list
	data, err := json.MarshalIndent(newConnections, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(connectionsFile, append(data, '\n'), 0600)
}

// renameConnection renames a connection
func renameConnection(oldName, newName string) error {
	connections := loadConnections()

	// Guard against duplicate names
	for _, conn := range connections {
		if conn.ConnName == newName && conn.ConnName != oldName {
			return fmt.Errorf("a connection named %q already exists", newName)
		}
	}

	// Find and rename the connection
	found := false
	for i, conn := range connections {
		if conn.ConnName == oldName {
			connections[i].ConnName = newName
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("connection not found: %s", oldName)
	}

	// Save updated list
	data, err := json.MarshalIndent(connections, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(connectionsFile, append(data, '\n'), 0600)
}

// updateConnection updates an existing connection
func updateConnection(oldName string, updatedConn DecryptedConnection) error {
	connections := loadConnections()

	// Find and update the connection
	found := false
	for i, conn := range connections {
		if conn.ConnName == oldName {
			// Create new connection with updated data
			var newConn Connection

			if EncryptConnectionDetails == "yes" {
				// Pack and encrypt the connection data
				packedData := packConnectionData(
					updatedConn.Environment,
					updatedConn.IPAddresses,
					updatedConn.Port,
					updatedConn.Username,
					updatedConn.Password,
					updatedConn.PromptUsername,
					updatedConn.PromptPassword,
				)

				encryptedData, err := encryptConnectionData(packedData)
				if err != nil {
					return err
				}

				newConn = Connection{
					ConnName:      updatedConn.ConnName,
					EncryptedData: encryptedData,
				}
			} else {
				// Store in plain text format
				newConn = Connection{
					ConnName:       updatedConn.ConnName,
					Environment:    updatedConn.Environment,
					IPAddresses:    updatedConn.IPAddresses,
					Port:           updatedConn.Port,
					Username:       updatedConn.Username,
					Password:       updatedConn.Password,
					PromptUsername: updatedConn.PromptUsername,
					PromptPassword: updatedConn.PromptPassword,
				}
			}

			connections[i] = newConn
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("connection not found: %s", oldName)
	}

	// Save updated list
	data, err := json.MarshalIndent(connections, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(connectionsFile, append(data, '\n'), 0600)
}

// migrateConnectionsEncryption rewrites the connections file so every entry
// matches the current EncryptConnectionDetails setting. Called once at startup;
// no-op when all entries are already in the correct format.
func migrateConnectionsEncryption() error {
	connections := loadConnections()
	if len(connections) == 0 {
		return nil
	}

	needsMigration := false
	for _, conn := range connections {
		if EncryptConnectionDetails == "yes" && conn.EncryptedData == "" {
			needsMigration = true
			break
		}
		if EncryptConnectionDetails == "no" && conn.EncryptedData != "" {
			needsMigration = true
			break
		}
	}
	if !needsMigration {
		return nil
	}

	// Decrypt every connection (decryptConnection handles both formats)
	var migrated []Connection
	for _, raw := range connections {
		dec, err := decryptConnection(raw)
		if err != nil {
			return fmt.Errorf("migrate: cannot read connection %q: %w", raw.ConnName, err)
		}

		var conn Connection
		if EncryptConnectionDetails == "yes" {
			packedData := packConnectionData(
				dec.Environment, dec.IPAddresses, dec.Port,
				dec.Username, dec.Password,
				dec.PromptUsername, dec.PromptPassword,
			)
			encryptedData, err := encryptConnectionData(packedData)
			if err != nil {
				return fmt.Errorf("migrate: cannot encrypt connection %q: %w", dec.ConnName, err)
			}
			conn = Connection{ConnName: dec.ConnName, EncryptedData: encryptedData}
		} else {
			conn = Connection{
				ConnName:       dec.ConnName,
				Environment:    dec.Environment,
				IPAddresses:    dec.IPAddresses,
				Port:           dec.Port,
				Username:       dec.Username,
				Password:       dec.Password,
				PromptUsername: dec.PromptUsername,
				PromptPassword: dec.PromptPassword,
			}
		}
		migrated = append(migrated, conn)
	}

	data, err := json.MarshalIndent(migrated, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(connectionsFile, append(data, '\n'), 0600)
}

// deduplicateConnectionNames detects duplicate ConnName values introduced by
// manual edits to the connections file and renames them by appending a random
// 5-character suffix (e.g. "test_con" → "test_con_7s3D6"). Silent no-op when
// no duplicates are found.
func deduplicateConnectionNames() error {
	connections := loadConnections()
	if len(connections) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	modified := false

	for i, conn := range connections {
		if !seen[conn.ConnName] {
			seen[conn.ConnName] = true
			continue
		}
		newName := uniqueConnectionName(conn.ConnName, seen)
		connections[i].ConnName = newName
		seen[newName] = true
		modified = true
	}

	if !modified {
		return nil
	}

	data, err := json.MarshalIndent(connections, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(connectionsFile, append(data, '\n'), 0600)
}

// uniqueConnectionName generates a name derived from original that is not
// present in taken, by appending "_XXXXX" (5 random alphanumeric chars).
// The original is truncated to 14 chars if needed so the result fits within
// the 20-character connection name limit.
func uniqueConnectionName(original string, taken map[string]bool) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	prefix := original
	if len(prefix) > 14 {
		prefix = prefix[:14]
	}
	for {
		b := make([]byte, 5)
		for i := range b {
			b[i] = chars[rand.Intn(len(chars))]
		}
		candidate := prefix + "_" + string(b)
		if !taken[candidate] {
			return candidate
		}
	}
}

// parseHosts parses comma-separated IP addresses and appends port if needed
func parseHosts(ipAddresses, port string) []string {
	hosts := strings.Split(ipAddresses, ",")
	for i := range hosts {
		hosts[i] = strings.TrimSpace(hosts[i])
	}

	// Append port if specified and not already in host
	if port != "" {
		for i, host := range hosts {
			if !strings.Contains(host, ":") {
				hosts[i] = host + ":" + port
			}
		}
	}

	return hosts
}

// configureCluster creates and configures a gocql ClusterConfig with performance optimizations
// timeout specifies the query timeout, connectTimeout specifies the connection timeout
func configureCluster(decConn DecryptedConnection, timeout, connectTimeout time.Duration) *gocql.ClusterConfig {
	hosts := parseHosts(decConn.IPAddresses, decConn.Port)
	cluster := gocql.NewCluster(hosts...)

	// Set authentication if provided
	if decConn.Username != "" && decConn.Password != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: decConn.Username,
			Password: decConn.Password,
		}
	}

	// Set timeouts
	cluster.Timeout = timeout
	cluster.ConnectTimeout = connectTimeout

	// Set consistency level
	cluster.Consistency = gocql.LocalQuorum

	// Performance optimization: Token-aware host selection policy
	// Routes queries directly to replica nodes, reducing network hops
	// DCAwareRoundRobinPolicy ensures queries prefer local datacenter
	// Note: Datacenter will be auto-detected by the driver
	cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(
		gocql.DCAwareRoundRobinPolicy(""),
	)

	// Performance optimization: Retry policy with exponential backoff
	// Automatically retries failed queries with increasing delays
	cluster.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{
		NumRetries: 3,
		Min:        100 * time.Millisecond,
		Max:        10 * time.Second,
	}

	// Performance optimization: Enable compression for large payloads
	cluster.Compressor = gocql.SnappyCompressor{}

	// Set reasonable page size for browse operations
	cluster.PageSize = 100

	return cluster
}

// testConnection tests a Cassandra connection
func testConnection(decConn DecryptedConnection) error {
	// Configure cluster with short timeouts for testing
	cluster := configureCluster(decConn, 5*time.Second, 5*time.Second)

	// Try to create a session
	session, err := cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("connection failed: %v", err)
	}
	defer session.Close()

	// Try a simple query to verify the connection works
	var clusterName string
	if err := session.Query("SELECT cluster_name FROM system.local").Idempotent(true).Scan(&clusterName); err != nil {
		return fmt.Errorf("query failed: %v", err)
	}

	return nil
}

// createBrowseSession creates a persistent Cassandra session for browsing
func createBrowseSession(decConn DecryptedConnection) (*gocql.Session, error) {
	// Configure cluster with longer timeouts for browse operations
	cluster := configureCluster(decConn, 30*time.Second, 30*time.Second)

	// Create and return the session (caller is responsible for closing)
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("connection failed: %v", err)
	}

	AppLog("established connection to Cassandra cluster")
	return session, nil
}

// closeBrowseSession safely closes a browse session
func closeBrowseSession(session *gocql.Session) {
	if session != nil && !session.Closed() {
		session.Close()
		AppLog("disconnected connection from Cassandra cluster")
	}
}

// executeQuery executes a CQL query and returns results
// This version creates a new session for each query (for standalone query execution)
func executeQuery(decConn DecryptedConnection, query string) ([]string, []map[string]interface{}, error) {
	// Create temporary session
	session, err := createBrowseSession(decConn)
	if err != nil {
		return nil, nil, err
	}
	defer closeBrowseSession(session)

	return executeQueryWithSession(session, query)
}

// executeQueryWithSession executes a CQL query using an existing session
// This is more efficient when multiple queries need to be executed
func executeQueryWithSession(session *gocql.Session, query string) ([]string, []map[string]interface{}, error) {
	return executeQueryWithSessionAndContext(context.Background(), session, query)
}

// parseSelectTableName extracts keyspace and table name from a SELECT query
// Returns keyspace, table, and a boolean indicating if extraction was successful
func parseSelectTableName(query string) (keyspace, table string, ok bool) {
	// Pattern to match: SELECT ... FROM [keyspace.]table
	// Handles quoted and unquoted identifiers
	matches := reSelectFrom.FindStringSubmatch(query)
	if matches == nil {
		return "", "", false
	}

	// Check for keyspace.table pattern (groups 1-4)
	if matches[1] != "" || matches[2] != "" {
		if matches[1] != "" {
			keyspace = matches[1]
		} else {
			keyspace = matches[2]
		}
		if matches[3] != "" {
			table = matches[3]
		} else {
			table = matches[4]
		}
		return keyspace, table, true
	}

	// Check for table-only pattern (groups 5-6)
	if matches[5] != "" {
		table = matches[5]
	} else if matches[6] != "" {
		table = matches[6]
	}

	return "", table, table != ""
}

// getPartitionKeyColumns retrieves the partition key column names for a table
func getPartitionKeyColumns(session *gocql.Session, keyspace, table string) ([]string, error) {
	var partitionKeys []string

	query := "SELECT column_name FROM system_schema.columns WHERE keyspace_name = ? AND table_name = ? AND kind = 'partition_key'"
	iter := session.Query(query, keyspace, table).Idempotent(true).Iter()

	var columnName string
	for iter.Scan(&columnName) {
		partitionKeys = append(partitionKeys, strings.ToLower(columnName))
	}

	if err := iter.Close(); err != nil {
		return nil, err
	}

	return partitionKeys, nil
}

// hasWhereClauseWithPartitionKey checks if the query has a WHERE clause that filters on partition key columns
func hasWhereClauseWithPartitionKey(query string, partitionKeys []string) bool {
	upperQuery := strings.ToUpper(query)

	// Check if WHERE clause exists
	whereIdx := strings.Index(upperQuery, "WHERE")
	if whereIdx == -1 {
		return false
	}

	// Extract WHERE clause (everything after WHERE, before ORDER BY, LIMIT, ALLOW FILTERING, etc.)
	whereClause := upperQuery[whereIdx+5:]

	// Remove trailing clauses
	for _, keyword := range []string{"ORDER BY", "LIMIT", "ALLOW FILTERING", "GROUP BY"} {
		if idx := strings.Index(whereClause, keyword); idx != -1 {
			whereClause = whereClause[:idx]
		}
	}

	// Check if any partition key column is referenced in the WHERE clause
	for _, pk := range partitionKeys {
		// Look for column name followed by comparison operator or IN
		pkPattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(strings.ToUpper(pk)) + `\b\s*(=|IN\s*\()`)
		if pkPattern.MatchString(whereClause) {
			return true
		}
	}

	return false
}

// hasLimitClause checks if the query already has a LIMIT clause
func hasLimitClause(query string) bool {
	upperQuery := strings.ToUpper(query)
	return reLimitClause.MatchString(upperQuery)
}

// capLimitAt200 checks if the query has a LIMIT clause with value > 200 and caps it at 200
func capLimitAt200(query string) string {
	// Pattern to match LIMIT followed by a number
	matches := reLimitValue.FindStringSubmatchIndex(query)
	if matches == nil {
		return query
	}

	// Extract the limit value (group 1 is at indices [2] and [3])
	limitStr := query[matches[2]:matches[3]]
	limitVal, err := strconv.Atoi(limitStr)
	if err != nil {
		return query
	}

	// If limit is greater than 200, replace it with 200
	if limitVal > 200 {
		return query[:matches[2]] + "200" + query[matches[3]:]
	}

	return query
}

// addSafetyLimit adds LIMIT 200 to SELECT queries that lack proper partition key filtering.
// If a query has proper partition key filtering in WHERE clause, any LIMIT is allowed.
// If no proper filtering, LIMIT is capped at 200 or added if missing.
func addSafetyLimit(session *gocql.Session, query string) string {
	upperQuery := strings.ToUpper(strings.TrimSpace(query))

	// Only process SELECT queries
	if !strings.HasPrefix(upperQuery, "SELECT") {
		return query
	}

	// Check if query has proper partition key filtering
	hasProperFiltering := false

	// Try to extract table name and check partition key usage
	keyspace, table, ok := parseSelectTableName(query)
	if ok && table != "" && keyspace != "" {
		// Get partition key columns
		partitionKeys, err := getPartitionKeyColumns(session, keyspace, table)
		if err == nil && len(partitionKeys) > 0 {
			// Check if WHERE clause uses partition keys
			hasProperFiltering = hasWhereClauseWithPartitionKey(query, partitionKeys)
		}
	}

	// If query has proper partition key filtering, allow any LIMIT
	if hasProperFiltering {
		return query
	}

	// No proper filtering - enforce LIMIT 200
	if hasLimitClause(query) {
		// Cap existing LIMIT at 200
		return capLimitAt200(query)
	}

	// Add LIMIT 200
	return appendLimit(query, 200)
}

// appendLimit adds a LIMIT clause to the query
func appendLimit(query string, limit int) string {
	// Remove trailing semicolon if present
	query = strings.TrimSpace(query)
	query = strings.TrimSuffix(query, ";")

	// Check for ALLOW FILTERING - LIMIT should come before it
	upperQuery := strings.ToUpper(query)
	if idx := strings.Index(upperQuery, "ALLOW FILTERING"); idx != -1 {
		return query[:idx] + fmt.Sprintf("LIMIT %d ", limit) + query[idx:]
	}

	return query + fmt.Sprintf(" LIMIT %d", limit)
}

// isSelectOnlyQuery reports whether query is a single SELECT statement.
// It strips comments and string literals before checking to prevent
// comment-hiding and multi-statement injection attacks.
func isSelectOnlyQuery(query string) bool {
	// Strip block comments  /* ... */
	cleaned := reBlockComment.ReplaceAllString(query, " ")
	cleaned = reLineComment.ReplaceAllString(cleaned, " ")
	cleaned = reStringLiteral.ReplaceAllString(cleaned, "''")

	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return false
	}

	if !reSelectStart.MatchString(cleaned) {
		return false
	}

	// Strip trailing semicolon — it is a valid CQL statement terminator.
	// Only a semicolon with content after it indicates a chained statement.
	cleaned = strings.TrimRight(cleaned, "; \t\n\r")

	// No semicolons allowed after stripping trailing one —
	// a remaining semicolon means a second statement was chained.
	return !strings.Contains(cleaned, ";")
}

// executeQueryWithSessionAndContext executes a CQL query with context support for cancellation
func executeQueryWithSessionAndContext(ctx context.Context, session *gocql.Session, query string) ([]string, []map[string]interface{}, error) {
	// Trim query
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil, fmt.Errorf("query cannot be empty")
	}

	// Determine if query is idempotent (SELECT queries are safe to retry)
	upperQuery := strings.ToUpper(query)
	isIdempotent := strings.HasPrefix(upperQuery, "SELECT")

	// Add safety LIMIT to SELECT queries without proper partition key filtering
	if isIdempotent {
		query = addSafetyLimit(session, query)
	}

	// Execute query with context and idempotency
	q := session.Query(query).WithContext(ctx).Idempotent(isIdempotent)
	iter := q.Iter()

	// Get column names from the query metadata
	columns := iter.Columns()
	var columnNames []string
	for _, col := range columns {
		columnNames = append(columnNames, col.Name)
	}

	// Fetch all rows (for SELECT queries)
	var rows []map[string]interface{}
	for {
		row := make(map[string]interface{})
		if !iter.MapScan(row) {
			break
		}
		rows = append(rows, row)
	}

	if err := iter.Close(); err != nil {
		return nil, nil, fmt.Errorf("query execution failed: %v", err)
	}

	return columnNames, rows, nil
}
