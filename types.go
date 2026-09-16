// Package mysql implements the MySQL storage adapter, query generator,
// DDL schema migrator, table introspector, and Dataset Studio compiler.
//
// File: types.go
// Usage:
//   This file defines the type conversion logic from core engine generic DataType enums
//   (model.TypeString, model.TypeInt, model.TypeDecimal, model.TypeJSON, etc.) to
//   MySQL-native SQL data types (VARCHAR, INT, DECIMAL, DATETIME, TINYINT(1), BLOB, etc.).
package mysql

import (
	"fmt"

	"github.com/SanjayDrop5528/models-go-engine/model"
	"github.com/SanjayDrop5528/models-go-engine/schema"
)

// ToMySQLType maps core generic DataType to MySQL SQL types.
//
// Purpose:
//   Translates an engine schema attribute into an idiomatic MySQL column data type string.
//
// Where it is used:
//   - Used by DDLGenerator when constructing CREATE TABLE and ADD COLUMN statements.
//
// When can it be used:
//   - When mapping generic models to MySQL table columns.
func ToMySQLType(attr schema.SchemaAttribute) string {
	switch attr.Type {
	case model.TypeString:
		if attr.Length > 0 {
			return fmt.Sprintf("VARCHAR(%d)", attr.Length)
		}
		return "VARCHAR(255)"

	case model.TypeText:
		return "TEXT"

	case model.TypeInt:
		return "INT"

	case model.TypeLong:
		return "BIGINT"

	case model.TypeFloat:
		return "DOUBLE"

	case model.TypeDecimal:
		if attr.Precision > 0 && attr.Scale > 0 {
			return fmt.Sprintf("DECIMAL(%d, %d)", attr.Precision, attr.Scale)
		}
		return "DECIMAL(10, 2)"

	case model.TypeBoolean:
		return "TINYINT(1)"

	case model.TypeDateTime:
		return "DATETIME"

	case model.TypeDate:
		return "DATE"

	case model.TypeTime:
		return "TIME"

	case model.TypeJSON:
		return "JSON"

	case model.TypeUUID:
		return "VARCHAR(36)"

	case model.TypeBinary:
		return "BLOB"

	default:
		return "VARCHAR(255)"
	}
}
