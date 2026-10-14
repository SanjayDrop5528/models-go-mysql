# models-go-mysql

> **MySQL 8.0 Database Adapter, DDL Migrator & DataSet SQL Compiler**

`models-go-mysql` provides the MySQL database adapter for `models-go-engine`. It supports MySQL 8.0 schema introspection, migration DDL generation (`ALTER TABLE`, `MODIFY COLUMN`), SQL dataset query compilation, and MySQL Stored Procedure DDL generation (`sp_...`).

## Prerequisites

- Go 1.20 or newer.
- MySQL 8.0 or newer.
- A MySQL DSN and a user with schema, query, and migration permissions as required by the operation.

## Usage Steps

1. Configure a MySQL DSN, for example `root:password@tcp(127.0.0.1:3306)/app_db?parseTime=true`.
2. Create the adapter with `mysql.NewMySQLAdapter(dsn)`.
3. Optionally restrict databases with `WithDatabases(...)`.
4. Call `Connect(ctx)` and check the returned error.
5. Register the adapter and `MySQLDataSetCompiler` with the engine.
6. Run CRUD, dataset preview/execute, or schema migration operations.
7. Run `go test ./...` to verify the adapter.

## Core Functionality

- MySQL connection and execution through the shared adapter contract.
- Schema introspection and transactional migration DDL.
- Query AST compilation to MySQL SQL.
- Stored procedure generation for saved datasets.

---

## 🛠️ Key Exported Functions & Methods Reference

### 1. `MySQLAdapter` ([`adapter.go`](./adapter.go))

Implements the `adapter.Adapter` interface for MySQL databases.

| Function / Method | Signature | Description |
| :--- | :--- | :--- |
| `NewMySQLAdapter` | `(dsn string) *MySQLAdapter` | Creates a new MySQL adapter instance using go-sql-driver/mysql. |
| `WithDatabases` | `(databases ...string) *MySQLAdapter` | Restricts operations to specific MySQL database names. |
| `Connect` | `(ctx context.Context) error` | Connects to MySQL server and pings connection. |
| `ApplySchemaChange` | `(ctx context.Context, p *plan.SchemaPlan) error` | Executes schema DDL statements within a transaction. |
| `Execute` | `(ctx context.Context, req execution.ExecutionRequest) (*execution.ExecutionResult, error)` | Runs queries or DDL statements against MySQL. |

---

### 2. `MySQLDataSetCompiler` ([`dataset_compiler.go`](./dataset_compiler.go))

Compiles Query AST into MySQL-compatible SQL queries (`SELECT ... FROM \`table\` AS \`alias\``) and Stored Procedures (`sp_...`).

| Function / Method | Signature | Description |
| :--- | :--- | :--- |
| `NewMySQLDataSetCompiler` | `() *MySQLDataSetCompiler` | Instantiates MySQL dataset compiler. |
| `Compile` | `(ctx context.Context, ast *planner.QueryAST, ds *domain.DataSet) (*compiler.CompiledPipeline, error)` | Compiles AST into MySQL query pipeline and stored procedure DDL. |

---

## 🚀 Usage Example

### Selecting fields inside dataset relations

`OBJECT` joins compile to `JSON_OBJECT`, while `MANY`/`JSON_ARRAY` joins compile to a correlated `JSON_ARRAYAGG`. In both cases, only relation fields present in `selected_list` are included in the nested JSON value. `NONE` continues to return the selected fields as flat columns.

```go
package main

import (
	"context"
	"fmt"

	"github.com/SanjayDrop5528/models-go-mysql"
)

func main() {
	dsn := "root:password@tcp(127.0.0.1:3306)/app_db?parseTime=true"
	adapter := mysql.NewMySQLAdapter(dsn)
	if err := adapter.Connect(context.Background()); err != nil {
		panic(err)
	}

	fmt.Println("Connected to MySQL Adapter:", adapter.Name())
}
```
