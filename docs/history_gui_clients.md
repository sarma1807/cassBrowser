# History of GUI Clients for **Apache Cassandra**

| App | Release | Discontinued | Supports | Code Type |
|---|---|---|---|---|
| DataStax DevCenter | October 2013 | 2017–2018 | Apache Cassandra & DSE | NOT Open Source |
| DataStax Studio | June 2016 | Not actively developed | DSE | NOT Open Source |
| DBeaver Community + SimbaCassandraJDBC driver | 2021 | April 2023 | Apache Cassandra & DSE | NOT Open Source ? |
| SimbaCassandraJDBC driver |  2021 | April 2023 | Apache Cassandra & DSE | Commercial product |
| DBeaver Community + cassandra-jdbc-wrapper by ing-bank | September 2023 | December 2025 | Apache Cassandra & DSE | NOT Open Source ? |
| cassBrowser | May 2026 | | Apache Cassandra & DSE | Open Source |

### cassBrowser is written in Go Lang
#### for UI cassBrowser uses cross-platform library Fyne.
#### cassBrowser uses gocql to connect to Apache Cassandra (gocql is a native Go library)

### In addition, cassBrowser has embedded DuckDB SQL Engine with native Go support
