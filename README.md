# models-go-mysql

> **MySQL 8.0 Database Adapter, DDL Migrator & DataSet SQL Compiler**

`models-go-mysql` provides the MySQL database adapter for `models-go-engine`. It supports MySQL 8.0 schema introspection, migration DDL generation (`ALTER TABLE`, `MODIFY COLUMN`), SQL dataset query compilation, and MySQL Stored Procedure DDL generation (`sp_...`).

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
