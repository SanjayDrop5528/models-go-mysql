package mysql

import (
	"fmt"
	"github.com/SanjayDrop5528/models-go-engine/query"
	"strings"
)

// QueryBuilder compiles query.Query into parameterized MySQL SQL queries with '?' placeholders.
type QueryBuilder struct{}

// BuildSelect compiles a SELECT query, returning the query string and argument slice.
func (b *QueryBuilder) BuildSelect(table string, q query.Query) (string, []any) {
	var args []any

	cols := "*"
	if len(q.Fields) > 0 {
		quoted := make([]string, len(q.Fields))
		for i, f := range q.Fields {
			quoted[i] = quoteIdent(f)
		}
		cols = strings.Join(quoted, ", ")
	}

	sql := fmt.Sprintf("SELECT %s FROM %s", cols, quoteIdent(table))

	if len(q.Filters) > 0 {
		whereClause, whereArgs := b.buildWhere(q.Filters, q.LogicalOp)
		sql += " WHERE " + whereClause
		args = append(args, whereArgs...)
	}

	if len(q.Sorts) > 0 {
		var sortClauses []string
		for _, s := range q.Sorts {
			order := "ASC"
			if s.Order == query.SortDesc {
				order = "DESC"
			}
			sortClauses = append(sortClauses, fmt.Sprintf("%s %s", quoteIdent(s.Field), order))
		}
		sql += " ORDER BY " + strings.Join(sortClauses, ", ")
	}

	if q.Pagination.Limit > 0 {
		sql += fmt.Sprintf(" LIMIT %d", q.Pagination.Limit)
	}
	if q.Pagination.Offset > 0 {
		sql += fmt.Sprintf(" OFFSET %d", q.Pagination.Offset)
	}

	return sql + ";", args
}

// BuildInsert compiles an INSERT statement.
func (b *QueryBuilder) BuildInsert(table string, data map[string]any) (string, []any) {
	var cols []string
	var placeholders []string
	var args []any

	for k, v := range data {
		cols = append(cols, quoteIdent(k))
		placeholders = append(placeholders, "?")
		args = append(args, v)
	}

	sql := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s);",
		quoteIdent(table),
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
	)

	return sql, args
}

// BuildUpdate compiles an UPDATE statement by ID.
func (b *QueryBuilder) BuildUpdate(table string, id any, data map[string]any) (string, []any) {
	var setClauses []string
	var args []any

	for k, v := range data {
		if k == "id" {
			continue
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = ?", quoteIdent(k)))
		args = append(args, v)
	}

	args = append(args, id)

	sql := fmt.Sprintf("UPDATE %s SET %s WHERE `id` = ?;",
		quoteIdent(table),
		strings.Join(setClauses, ", "),
	)

	return sql, args
}

// BuildDelete compiles a DELETE statement by ID.
func (b *QueryBuilder) BuildDelete(table string, id any) (string, []any) {
	sql := fmt.Sprintf("DELETE FROM %s WHERE `id` = ?;", quoteIdent(table))
	return sql, []any{id}
}

func (b *QueryBuilder) buildWhere(filters []query.Filter, op query.LogicalOp) (string, []any) {
	var clauses []string
	var args []any

	joinOp := " AND "
	if op == query.OpOr {
		joinOp = " OR "
	}

	for _, f := range filters {
		clause, fArgs := b.buildCondition(f)
		clauses = append(clauses, clause)
		args = append(args, fArgs...)
	}

	return strings.Join(clauses, joinOp), args
}

func (b *QueryBuilder) buildCondition(f query.Filter) (string, []any) {
	col := quoteIdent(f.Field)

	switch f.Op {
	case query.OpEq:
		return fmt.Sprintf("%s = ?", col), []any{f.Value}
	case query.OpNeq:
		return fmt.Sprintf("%s != ?", col), []any{f.Value}
	case query.OpGt:
		return fmt.Sprintf("%s > ?", col), []any{f.Value}
	case query.OpGte:
		return fmt.Sprintf("%s >= ?", col), []any{f.Value}
	case query.OpLt:
		return fmt.Sprintf("%s < ?", col), []any{f.Value}
	case query.OpLte:
		return fmt.Sprintf("%s <= ?", col), []any{f.Value}
	case query.OpLike:
		return fmt.Sprintf("%s LIKE ?", col), []any{f.Value}
	case query.OpILike:
		return fmt.Sprintf("%s LIKE ?", col), []any{f.Value}
	case query.OpIsNull:
		return fmt.Sprintf("%s IS NULL", col), nil
	case query.OpIsNotNull:
		return fmt.Sprintf("%s IS NOT NULL", col), nil
	case query.OpBetween:
		return fmt.Sprintf("%s BETWEEN ? AND ?", col), []any{f.Value, f.ValueTo}
	case query.OpIn:
		if slice, ok := f.Value.([]any); ok && len(slice) > 0 {
			placeholders := make([]string, len(slice))
			for i := range slice {
				placeholders[i] = "?"
			}
			return fmt.Sprintf("%s IN (%s)", col, strings.Join(placeholders, ", ")), slice
		}
		return fmt.Sprintf("%s = ?", col), []any{f.Value}
	default:
		return fmt.Sprintf("%s = ?", col), []any{f.Value}
	}
}
