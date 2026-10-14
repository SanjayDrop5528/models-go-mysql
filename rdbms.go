// Package mysql implements the MySQL storage adapter, query generator,
// DDL schema migrator, table introspector, and Dataset Studio compiler.
//
// File: rdbms.go
// Usage:
//
//	This file provides fluent relational query integration methods on MySQLAdapter,
//	exposing *rdbms.DB and *rdbms.SelectQuery configured with the MySQL dialect.
package mysql

import (
	"context"

	"github.com/SanjayDrop5528/models-go-engine/rdbms"
	"github.com/SanjayDrop5528/models-go-engine/rdbms/dialect"
)

// RDBMS returns an active *rdbms.DB instance tied to this adapter's DB and MySQL dialect.
//
// Purpose:
//
//	Creates a fluent RDBMS query executor wired to the live MySQL connection pool.
//
// Where it is used:
//   - Used by advanced query builders, reporting services, and multi-table join routines.
//
// When can it be used:
//   - When executing complex multi-table SQL queries with MySQL backtick quoting and dialect rules.
//
// Parameters:
//   - ctx (context.Context): Input value used by the operation.
//
// Returns:
//   - result1 (*rdbms.DB): Returned value.
//   - result2 (error): Returned value.
//
// Business Rules:
//   - Follows the invariants documented by RDBMS and its owning package.
//
// Side Effects:
//   - Does not intentionally mutate caller-owned input; configured dependencies may still be consulted.
//
// Errors:
//   - Returns validation, configuration, context, or underlying dependency errors.
//
// Note:
//   - The context controls cancellation and request lifetime.
func (a *MySQLAdapter) RDBMS(ctx context.Context) (*rdbms.DB, error) {
	db, err := a.getDB(ctx)
	if err != nil {
		return nil, err
	}
	return rdbms.NewDB(db, dialect.NewMySQL()), nil
}

// NewSelect returns a new rdbms.SelectQuery configured with the MySQL dialect.
//
// Purpose:
//
//	Provides a chainable SELECT query builder targeted specifically for MySQL dialect syntax.
//
// Where it is used:
//   - Used in relational query construction and service query handlers.
//
// When can it be used:
//   - When writing fluent SELECT statements with joins, groupings, or subqueries.
//
// Parameters:
//   - ctx (context.Context): Input value used by the operation.
//
// Returns:
//   - result1 (*rdbms.SelectQuery): Returned value.
//   - result2 (error): Returned value.
//
// Business Rules:
//   - Follows the invariants documented by NewSelect and its owning package.
//
// Side Effects:
//   - Does not intentionally mutate caller-owned input; configured dependencies may still be consulted.
//
// Errors:
//   - Returns validation, configuration, context, or underlying dependency errors.
//
// Note:
//   - The context controls cancellation and request lifetime.
func (a *MySQLAdapter) NewSelect(ctx context.Context) (*rdbms.SelectQuery, error) {
	rdb, err := a.RDBMS(ctx)
	if err != nil {
		return nil, err
	}
	return rdb.NewSelect(), nil
}
