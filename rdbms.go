// Package mysql implements the MySQL storage adapter, query generator,
// DDL schema migrator, table introspector, and Dataset Studio compiler.
//
// File: rdbms.go
// Usage:
//   This file provides fluent relational query integration methods on MySQLAdapter,
//   exposing *rdbms.DB and *rdbms.SelectQuery configured with the MySQL dialect.
package mysql

import (
	"context"

	"github.com/SanjayDrop5528/models-go-engine/rdbms"
	"github.com/SanjayDrop5528/models-go-engine/rdbms/dialect"
)

// RDBMS returns an active *rdbms.DB instance tied to this adapter's DB and MySQL dialect.
//
// Purpose:
//   Creates a fluent RDBMS query executor wired to the live MySQL connection pool.
//
// Where it is used:
//   - Used by advanced query builders, reporting services, and multi-table join routines.
//
// When can it be used:
//   - When executing complex multi-table SQL queries with MySQL backtick quoting and dialect rules.
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
//   Provides a chainable SELECT query builder targeted specifically for MySQL dialect syntax.
//
// Where it is used:
//   - Used in relational query construction and service query handlers.
//
// When can it be used:
//   - When writing fluent SELECT statements with joins, groupings, or subqueries.
func (a *MySQLAdapter) NewSelect(ctx context.Context) (*rdbms.SelectQuery, error) {
	rdb, err := a.RDBMS(ctx)
	if err != nil {
		return nil, err
	}
	return rdb.NewSelect(), nil
}
