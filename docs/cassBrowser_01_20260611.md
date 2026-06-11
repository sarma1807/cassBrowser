# cassBrowser — The Cassandra GUI that database engineers actually needed

If you work with **#Apache #Cassandra**, you already know the frustration.

CQL shell is powerful — but staring at raw terminal output for wide tables with complex types is painful.
Most third-party GUI tools are bloated, require Java runtimes, or charge for features that should be free.
And when you need to analyze exported data? You're juggling files, Python scripts, and duct tape.

**cassBrowser changes that.**

It's a free, open-source, cross-platform Cassandra browser written in Go — and it's genuinely impressive engineering.

---

## ⚙️ What Makes It Technically Solid

- **Single portable binary** — No JVM. No `pip install`. No Docker. Download, run, done.
  Works on RHEL Linux, Ubuntu, macOS, and Windows out of the box.

- **Fyne + Go architecture** — Native compiled GUI with platform-specific system integrations.
  The UI feels snappy because it IS native — not a web app wrapped in Electron.

- **🦆 DuckDB + Parquet integration** *(this one is genuinely clever)*
  Export any Cassandra table to Apache **#Parquet** format with column-level selection and WHERE filters.
  Then run DuckDB SQL queries against those Parquet files — in-memory, blazing fast.
  You can even `JOIN` across multiple exported Parquet files.
  This effectively gives you ad-hoc analytical SQL on top of Cassandra data without any ETL pipeline.

- **Token-aware, DC-aware connectivity** — Proper Cassandra driver behavior.
  Not just a thin CQL wrapper — it respects your cluster topology.

- **🔒 Security-first design**
  - AES-256-GCM encryption for stored credentials using machine-specific keys
  - File permissions enforced at `0600` (owner read/write only)
  - Process umask set to `0077` at startup
  - SELECT-only query enforcement with injection prevention

  > No accidental data mutations. Ever.

---

## 🖥️ The Browsing Experience

| Feature | Detail |
|---|---|
| 🌳 Hierarchical tree view | Environments → Clusters → Keyspaces → Tables → Columns |
| 📋 Full DDL display | `CREATE TABLE` schema introspection at a glance |
| 📄 Paginated data grid | Configurable page size with auto-calculated column widths |
| ✍️ Multi-line CQL input | Real-time query results |
| 🛡️ Auto LIMIT application | No accidental full-table scans |
| 🔍 Detail panel | Inspect individual row values |
| 🔌 Connection indicators | Idle / Connecting / Connected state at a glance |
| ✅ Test Connection | Validate before you commit — no surprises |

---

## ✨ Polished Details

- 🎨 **27 color themes** — including colorblind-safe variants
- 🔡 Configurable font sizes for both UI and data grid
- ◀▶ Collapsible panels for maximizing your data view
- 📅 Daily rotating log files
- 📊 CPU/memory usage monitoring
- 💾 Window size persistence across sessions

---

## 👥 Who Is This For?

| Role | Use Case |
|---|---|
| 📦 Data Engineers | Browse schemas, export datasets to Parquet, run DuckDB analytics |
| 🛢️ DBAs | Inspect cluster topology, keyspace configs, replication strategies |
| 🧑‍💻 Backend Developers | Validate data, debug queries, browse tables during development |
| 🐚 cqlsh users | Anyone who has ever wished cqlsh had a GUI mode |

---

## 💡 My Take

The **DuckDB + Parquet integration** alone makes this worth watching.

Think about what that enables: you export Cassandra table snapshots to Parquet, then run complex analytical SQL — `GROUP BY`, window functions, multi-table `JOIN`s — entirely in-memory on your local machine. No Spark cluster. No data warehouse. No ETL job.

> That's not a minor feature. That's a **workflow transformation** for anyone doing ad-hoc analysis on Cassandra data.

The security model is also thoughtfully designed — credential encryption with machine-specific keys, strict file permissions enforced at the OS level, and read-only query enforcement means you can safely hand this tool to anyone on your team without worrying about accidental writes.

This is the kind of project that deserves far more attention than its current ⭐ count suggests.

---

## 🚀 Call to Action

If you work with Apache Cassandra — or you know someone who does — give this project a **⭐ STAR** on GitHub.

It's **free**. It's **open source**. It's **well-engineered**.
And right now it's sitting at just under 10 stars, which is a crime.

🔗 **https://github.com/sarma1807/cassBrowser**

> A star costs you nothing and helps a solid piece of engineering get the visibility it deserves.

---

*#ApacheCassandra #DatabaseTools #OpenSource #GoLang #DataEngineering #DuckDB #Parquet #DeveloperTools #BackendEngineering*
