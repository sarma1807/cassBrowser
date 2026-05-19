# Cassandra Browser — Application Features

A desktop GUI application for browsing Apache Cassandra clusters, built in Go using the Fyne cross-platform framework.

---

## Table of Contents

1. [User Interface Layout](#1-user-interface-layout)
2. [Connection Management](#2-connection-management)
3. [Cassandra Browsing](#3-cassandra-browsing)
4. [Data Viewing and Pagination](#4-data-viewing-and-pagination)
5. [CQL Query Execution](#5-cql-query-execution)
6. [Data Export](#6-data-export)
7. [Parquet File Querying — DuckDB SQL](#7-parquet-file-querying--duckdb-sql)
8. [Security and Encryption](#8-security-and-encryption)
9. [Themes and Appearance](#9-themes-and-appearance)
10. [Menus, Controls and Keyboard Shortcuts](#10-menus-controls-and-keyboard-shortcuts)
11. [Status Bar and Feedback](#11-status-bar-and-feedback)
12. [Application Statistics](#12-application-statistics)
13. [Logging](#13-logging)
14. [Runtime File System Layout](#14-runtime-file-system-layout)
15. [Performance and Reliability](#15-performance-and-reliability)
16. [Build Configuration](#16-build-configuration)

---

## 1. User Interface Layout

### Main Window
- Title bar displays **"{APP_DISPLAY_NAME} by oramad"**
- Two-panel split layout :
  - **Left panel** (30%) — connection tree
  - **Right panel** (70%) — details, forms, data, query results
- **Status bar** at the bottom showing live status messages and application tagline
- Debounced resize handling (150 ms) prevents excessive redraws on window resize
- Window size persisted across sessions; restores on next launch
- Centered on screen at first launch (default 1280 × 720)

### Left Panel — Connection Tree
- Hierarchical tree with environments as collapsible branches
- Top-level nodes :
  - **Add New Connection**
  - **Disconnect All**
  - Environment branches (with connection count in parentheses)
  - **Data In Local Files** — browse local Parquet files
  - **JOIN Data In Local Files** — multi-file SQL JOIN query
  - **App Stats** — live application statistics
- Per-connection nodes show current state : idle, connecting (spinner icon), or connected
- Keyspace nodes expand under a connected connection
- Table nodes expand under each keyspace
- Visual icons distinguish node types (folder, database, table, document)
- Left panel can be collapsed/expanded via ◀/▶ toggle buttons

### Right Panel — Detail Area
- Dynamic content area; renders different views depending on tree selection :
  - About page, connection form, cluster info, keyspace info, table details, data grid, query form, extraction form, local file query, stats view, settings forms
- **Special output panel** (overlay at bottom) for extraction progress and result messages :
  - Success messages — green
  - Error messages — red
  - Info messages — cyan
  - Warning messages — amber
  - Close button to dismiss; "View" button appears during active extraction

---

## 2. Connection Management

### Connection Operations
- **Add** new connections via validated form
- **Edit** existing connection details
- **Delete** connections with confirmation dialog
- **Rename** connections with name validation
- Connections grouped and displayed by **environment**

### Connection Form Fields

| Field | Required | Rules |
|---|---|---|
| Connection Name | Yes | 5–20 chars, alphanumeric, `_`, `-` |
| Environment | Yes | 3–20 chars, UPPERCASE only |
| IP Addresses | Yes | 3–100 chars, comma-separated |
| Port | Yes | Numeric only, default 9042 |
| Username | No | 3–30 chars if provided |
| Password | No | 3–50 chars if provided, masked |
| Prompt Username | No | Checkbox — prompt at connect time |
| Prompt Password | No | Checkbox — prompt at connect time |

- Real-time field validation on focus loss with inline error messages
- **Test Connection** button — inline result display (Testing… → ✓ success or ✗ error)

### Connection Driver Options
- Token-aware host selection (direct routing to replica)
- Datacenter-aware round-robin load balancing
- Exponential backoff retry — 3 retries, 100 ms–10 s range
- Snappy compression for large payloads
- Password authentication via Cassandra PasswordAuthenticator
- SSL/TLS support (configurable via gocql)
- Query timeout : 5 s (test), 30 s (browse)
- Connection timeout : 5 s (test), 30 s (browse)
- Page size : 100 rows (browsing), adaptive (extraction)
- Local quorum consistency level

### Connection Storage
- Stored in `~/.cassBrowser/cassBrowser.connections` (JSON)
- Optional AES-256-GCM encryption of credential fields
- Automatic startup migration for encryption format changes
- Automatic deduplication of duplicate connection names on load
- File permissions : 0600 (owner read/write only)

---

## 3. Cassandra Browsing

### Cluster Information
- Cluster name, release version
- Data center and rack
- Local node IP address
- Total keyspace count and user keyspace count

### Keyspace Browsing
- Lists all keyspaces (system and user), sorted alphabetically
- Keyspace detail view : replication strategy, durable writes

### Table Browsing
- Lists all tables in a keyspace, sorted alphabetically
- Full **CREATE TABLE DDL** display with copy-able plain text

### Column Information
- Column name, data type, kind (partition key, clustering, regular, static)
- Position in the partition or clustering key
- Clustering order (ASC / DESC)
- Column sort order in display :
  1. Partition keys (by position)
  2. Clustering keys (by position)
  3. Regular and static columns (alphabetically)

### Supported Cassandra Data Types
`int`, `bigint`, `float`, `double`, `decimal`, `varint`, `text`, `varchar`, `ascii`, `blob`,
`timestamp`, `date`, `time`, `duration`, `uuid`, `timeuuid`, `boolean`, `inet`,
`list`, `set`, `map`, `tuple`, frozen variants

---

## 4. Data Viewing and Pagination

### Data Grid
- Paginated table display — 20 rows per page (configurable)
- Automatic column width calculation based on content (samples first 200 rows)
- Column width constraints : minimum 30 px, maximum 300 px
- Row number column (auto-sized)
- Header row with column names
- Null values represented clearly

### Pagination Controls
- Previous / Next page buttons
- Current page indicator : "Page X of Y"
- Note displayed about automatic query limits

### Data Formatting
- Collections (list, set, map) rendered as readable text
- UUIDs, timestamps, dates formatted for display
- Blob data represented safely
- Long values truncated for readability

---

## 5. CQL Query Execution

### Query Features
- Multi-line CQL query input with **Safe Entry** widget (patches a Fyne v2.4.4 word-move cursor crash)
- Execute and Clear buttons
- Query results displayed in the paginated data grid
- Row count shown after execution
- Column metadata extracted from live query results

### Query Safety Enforcement
- **SELECT-only** validation — other statement types rejected
- Multi-statement injection prevention (comment and string literal stripping before validation)
- Automatic **LIMIT 200** applied when no partition key filter detected in WHERE clause
- Existing LIMIT values capped at 200 if no proper partition key filtering
- Partition key analysis from WHERE clause before execution
- Idempotent query flag set for all SELECT statements
- Context-based cancellation support

---

## 6. Data Export

### Export to Apache Parquet
- Full table extraction to `.parquet` files
- **Column selection** — choose specific columns to include
- Optional **WHERE clause** with ALLOW FILTERING
- Cassandra type metadata embedded in Parquet file metadata (enables round-trip type fidelity)
- Progress tracking with live status messages in output panel
- Cancellation support — partial file retained on cancel

### Parquet Writer Optimisation
- Adaptive page sizing : 5 000 rows default; 2 000 rows for wide-column tables
- Wide-column detection : `blob`, `text`, `varchar`, `ascii`, `list`, `set`, `map`, `tuple`
- Column names sorted alphabetically to match parquet-go schema ordering
- Row-group batching for memory efficiency
- NULL values handled as optional Parquet columns

### Export File Management
- Output location : `<working-directory>/data/`
- Filename customisable with date/time defaults
- File creation tracked via Linux `statx` syscall
- File permissions : 0600

---

## 7. Parquet File Querying — DuckDB SQL

### Local File Browsing
- Scans `<working-directory>/data/` for `.parquet` files on demand
- File list sorted alphabetically
- "No files found" state handled gracefully

### Single-File Querying
- Select a Parquet file from the tree
- Enter a SQL SELECT query
- DuckDB executes the query in-memory against the file
- Results displayed in the paginated data grid
- Query timing displayed : file load duration + query execution duration
- DuckDB memory usage tracked and logged

### Multi-File JOIN Querying
- Select multiple Parquet files with user-defined aliases
- Write SQL JOIN queries referencing files by alias
- Results displayed in data grid
- Timing and memory stats shown

### DuckDB Safety
- SELECT-only query validation
- Safe path handling (quote-escaped file paths)
- View creation per query for clean alias mapping
- Per-query in-memory DuckDB instance

---

## 8. Security and Encryption

### Connection Credential Encryption
- AES-256-GCM encryption for stored connection credentials (configurable at build time)
- Encryption key derived **per machine** — not password-based :
  - Inputs : hostname + MAC address + OS machine-id
  - Key derivation : PBKDF2 with SHA-256, 100 000 iterations
- Encrypted fields Base64-encoded with embedded nonce
- Connection files are **not portable** between machines when encryption is enabled
- Supports mixed files (some encrypted, some plain-text entries)
- Automatic migration at startup when encryption mode changes

### File System Security
- Config directory : 0700 (owner access only)
- Connections and settings files : 0600
- Process umask set to 0077 at startup (all new files default to owner-only)
- Working directory tested for write access before use

### Query Security
- Regex-based CQL validation with comment and string literal stripping
- Multi-statement injection prevention
- SELECT-only enforcement for both CQL and DuckDB SQL
- Partition key filter enforcement (auto-LIMIT as safety net)

### Display Check
- Application refuses to start without a GUI display (checks `DISPLAY` / `WAYLAND_DISPLAY`)

---

## 9. Themes and Appearance

### Color Schemes — 27 Total

**Dark Themes (9)**

| Theme | Description |
|---|---|
| Midnight Gold | Default dark theme |
| Ocean Blue | Blue-toned dark |
| Forest Green | Green-toned dark |
| Purple Haze | Purple-toned dark |
| Crimson Night | Red-toned dark |
| Teal Shadow | Teal-toned dark |
| Copper Rust | Copper-toned dark |
| Slate Gray | Neutral gray dark |
| Neon Pink | High-contrast pink dark |

**Light Themes (9)**

| Theme | Description |
|---|---|
| Classic Light | Standard light theme |
| Warm Cream | Warm off-white |
| Mint Fresh | Green-tinted light |
| Rose Petal | Pink-tinted light |
| Sky Blue | Blue-tinted light |
| Lavender Mist | Purple-tinted light |
| Sunny Day | Yellow-tinted light |
| Clean White | Pure white |
| Coral Reef | Coral-tinted light |

**Colorblind-Safe Themes (9)**

| Theme | Accessibility Target |
|---|---|
| Blue Orange | General colorblind-safe |
| Blue Yellow | General colorblind-safe |
| High Contrast | Maximum contrast |
| Monochrome Blue | Single-hue blue |
| Warm Safe | Warm tones, safe palette |
| Deuteranopia | Green-blind |
| Protanopia | Red-blind |
| Tritanopia | Blue-blind |
| Grayscale | No color dependency |

### Font Settings
- **App font size** — scales the entire UI
- **Grid font size** — scales data table cells independently
- Font sizes persisted in settings file
- Monospace font enforced throughout
- Dynamic title size updates on change

### Settings Persistence
- Selected theme, font sizes, and working directory all saved to `~/.cassBrowser/cassBrowser.settings`

---

## 10. Menus, Controls and Keyboard Shortcuts

### Menu Bar

**Connections**
- Add New Connection
- Disconnect All

**Settings**
- Font Size
- Colors
- Working Directory
- Enable / Disable App Stats

**Help**
- About this App

### Safe Entry Widget
- Patches a crash in Fyne v2.4.4 triggered by `Ctrl+Left` / `Ctrl+Right` word-move when cursor is at certain positions
- Applied to all multi-line query and form entry fields

### Working Directory Configuration
- Set from Settings menu; right panel used for input (no popup dialogs)
- Directory validated for write access before saving
- Sub-directories `appLogs/`, `data/`, `temp/` created automatically with 0700 permissions

---

## 11. Status Bar and Feedback

### Status Bar Messages

| State | Message |
|---|---|
| Idle | App tagline |
| Connection selected | Selected Connection : [name] |
| Connected | Connected to : [name] |
| Connection failed | Connection failed : [name] |
| Keyspace selected | Keyspace : [name] |
| Table selected | Table : [keyspace].[table] |
| Local file selected | Local file : [path] |
| No parquet files | No parquet files are found |
| JOIN mode | JOIN Data In Local Files |
| Stats view | App Stats |
| Color settings | Color Settings — Select a theme |
| Font settings | Font Settings — Configure title font |
| Working directory | Working Directory Setup |
| About | About [AppName] |
| Test result | Test Connection : [status] |
| Disconnect all | Disconnected from all Cassandra connections |

### Special Output Panel Messages
- **Success** (green, ✓ prefix) — extraction complete, file saved
- **Error** (red, ✗ prefix) — extraction failed, query errors
- **Info** (cyan) — general progress updates
- **Warning** (amber, bullet points) — caution notices

---

## 12. Application Statistics

- Optional feature — enabled/disabled from the Settings menu at runtime
- Metrics captured every **60 seconds** :
  - App memory usage (`Sys` bytes via `syscall.Rusage`)
  - CPU usage percentage (user + system time delta / elapsed time)
  - DuckDB memory usage (via `duckdb_memory()` system function)
- Stats written to daily log files in `<working-directory>/appLogs/`
- File format : `appStats_YYYYMMDD.log`
- Header line documents metric names : `am` (app memory), `ac` (app CPU %), `dm` (DuckDB memory)
- **Line chart visualisation** in the App Stats panel :
  - Custom widget with auto-scaling Y-axis
  - X-axis time labels
  - Configurable title, unit, and line colour
  - Grid background

---

## 13. Logging

- Daily rotating log files in `<working-directory>/appLogs/`
- File format : `cassBrowser_YYYYMMDD.log`
- Log entry format : `timestamp | message`
- Events logged :
  - Application startup
  - Connection test results
  - Extraction start, completion, cancellation
  - Window close

---

## 14. Runtime File System Layout

```
~/.cassBrowser/
├── cassBrowser.settings        # JSON — theme, font sizes, working dir, stats toggle
└── cassBrowser.connections     # JSON — connection definitions (optionally encrypted)

<working-directory>/
├── appLogs/
│   ├── cassBrowser_YYYYMMDD.log    # Application event log (daily rotation)
│   └── appStats_YYYYMMDD.log       # Stats capture log (if enabled)
├── data/
│   └── *.parquet                   # Extracted Parquet files
└── temp/                           # Temporary working files
```

### Settings File Fields
| Field | Description |
|---|---|
| `window_width` / `window_height` | Persisted window dimensions |
| `theme_scheme` | Active colour theme name |
| `working_directory` | Path for logs and data output |
| `app_font_size` | Global UI font scale |
| `grid_font_size` | Data grid font scale |
| `capture_app_stats` | Boolean — stats capture enabled |

---

## 15. Performance and Reliability

- **Debounced layout** (150 ms) — prevents excessive panel redraws during window resize
- **Lazy loading** — keyspaces and tables loaded only when a connection node is expanded
- **Context-based cancellation** — all long-running operations (extraction, queries) are cancellable
- **Sample-based column sizing** — column widths calculated from first 200 rows only (avoids O(n×m) cost)
- **Token-aware routing** — queries routed directly to the owning replica
- **Snappy compression** — reduces network payload for large result sets
- **Adaptive extraction page size** — automatically reduced for wide-column tables
- **Parallel startup** — connection migrations run concurrently with Fyne initialisation
- **Row-group batching** — Parquet writes use batched row groups for memory efficiency
- **Per-query DuckDB instance** — each SQL query gets a fresh in-memory DuckDB instance, preventing state leakage

---

## 16. Build Configuration

All values injected at compile time via `-ldflags -X`; no configuration files needed at runtime.

| Property | `app.properties` Key | Default |
|---|---|---|
| Binary name | `APP_COMPILE_NAME` | `cassBrowser` |
| Display name | `APP_DISPLAY_NAME` | `Cassandra Browser` |
| Version | `APP_VERSION` | `1.0` |
| Build date | `APP_VERSION_DATE` | — |
| Encrypt connections | `ENCRYPT_CONNECTION_DETAILS` | `yes` |

- Binary is fully standalone — all configuration baked in at build time
- Changing `app.properties` requires a rebuild
- Config directory name matches `APP_COMPILE_NAME`
