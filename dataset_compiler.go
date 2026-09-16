// Package mysql provides dataset compilation logic for MySQL databases.
//
// Usage:
// This file transforms an engine QueryAST and DataSet definition into dialect-specific MySQL
// SELECT queries, stored procedures, or stored functions returning JSON. It supports:
// 1. Backtick identifier quoting (`table`.`field`).
// 2. Table joins (INNER, LEFT, RIGHT).
// 3. Custom column formulas and MySQL built-in calculation/aggregation functions.
// 4. Runtime parameter substitution and routine DDL generation (CREATE PROCEDURE / FUNCTION).
package mysql

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/SanjayDrop5528/models-go-engine/adapter"
	"github.com/SanjayDrop5528/models-go-engine/dataset/compiler"
	"github.com/SanjayDrop5528/models-go-engine/dataset/domain"
	"github.com/SanjayDrop5528/models-go-engine/dataset/planner"
)

// MySQLDataSetCompiler compiles QueryAST into MySQL SQL and Stored Procedures/Functions.
type MySQLDataSetCompiler struct{}

// NewMySQLDataSetCompiler creates a new MySQL dataset compiler instance.
//
// Purpose:
// Instantiates a MySQLDataSetCompiler capable of translating an engine QueryAST into MySQL SQL.
//
// Where it is used:
// Used in MySQLAdapter.CompileDataSet, MySQLAdapter.DataSetCompiler, and can be used directly
// in test suites or engine service registrations.
//
// When can it be used:
// Can be used during application initialization or adapter registration to provide MySQL compilation support.
func NewMySQLDataSetCompiler() *MySQLDataSetCompiler {
	return &MySQLDataSetCompiler{}
}

// Compile compiles the QueryAST into MySQL SQL and stored DDL.
//
// Purpose:
// Compiles an engine QueryAST into an executable MySQL SQL query, a reference parameterized pipeline,
// and a routine DDL statement (procedure or function) depending on the dataset's SaveMode.
//
// Where it is used:
// Invoked by DataSetService.Preview, DataSetService.Save, and MySQLAdapter.CompileDataSet.
//
// When can it be used:
// Can be used whenever a DataSet definition has been validated and planned and needs to be executed
// or persisted in a MySQL database.
func (c *MySQLDataSetCompiler) Compile(ctx context.Context, ast *planner.QueryAST, ds *domain.DataSet) (*compiler.CompiledPipeline, error) {
	if ast == nil {
		return nil, domain.NewError(domain.ErrPipelineCompilationFailed, "cannot compile nil AST")
	}

	saveMode := ds.SaveMode
	if saveMode == "" {
		saveMode = domain.SaveModeQuery
	}

	execQuery := c.buildSelectSQL(ast, false, false)
	refQuery := c.buildSelectSQL(ast, true, false)
	routineQuery := c.buildSelectSQL(ast, false, true)

	ddl := c.buildDDL(ds.ReferenceName, routineQuery, ast.Parameters, saveMode)

	return &compiler.CompiledPipeline{
		ExecutableQuery:   execQuery,
		ReferencePipeline: refQuery,
		Parameters:        ast.Parameters,
		DDLStatement:      ddl,
		SaveMode:          saveMode,
		Driver:            "mysql",
	}, nil
}

// buildSelectSQL constructs the MySQL SELECT query.
//
// Purpose:
// Builds the complete MySQL SQL query string including backtick-quoted projections, calculations, JOIN clauses,
// ON filters, WHERE filters, GROUP BY groupings, and HAVING clauses.
//
// Where it is used:
// Called internally by Compile to generate executable queries, parameter-templated pipelines, and routine bodies.
//
// When can it be used:
// Can be used during compilation whenever a QueryAST must be serialized into a MySQL SELECT statement.
func (c *MySQLDataSetCompiler) buildSelectSQL(ast *planner.QueryAST, parameterized, isRoutine bool) string {
	var selectCols []string

	// 1. Projections
	for _, p := range ast.Projections {
		colExpr := fmt.Sprintf("`%s`.`%s`", p.SourceTable, p.SourceField)
		if p.Alias != "" && p.Alias != p.SourceField {
			colExpr += fmt.Sprintf(" AS `%s`", p.Alias)
		}
		selectCols = append(selectCols, colExpr)
	}

	// 2. Custom Columns
	for _, cc := range ast.CustomColumns {
		// When query has GROUP BY, row-level calculations cannot be projected unless grouped
		if len(ast.GroupBy) > 0 && !cc.IsAggregate {
			inGroupBy := false
			for _, g := range ast.GroupBy {
				if strings.EqualFold(g.Field, cc.Alias) || strings.EqualFold(g.Field, cc.Label) {
					inGroupBy = true
					break
				}
			}
			if !inGroupBy {
				continue
			}
		}

		expr := cc.Expression
		if cc.Function != nil && cc.Function.MySQLExpression != "" {
			expr = renderMySQLFunctionExpression(cc.Function.MySQLExpression, cc.Operands)
		} else if expr == "" && cc.Function != nil {
			expr = buildMySQLFunctionExpression(cc.Function.Name, cc.Operands)
		} else if expr == "" && cc.IsAggregate {
			expr = buildMySQLFunctionExpression(cc.FunctionName, cc.Operands)
		} else if expr == "" && cc.FunctionName != "" {
			expr = buildMySQLFunctionExpression(cc.FunctionName, cc.Operands)
		}

		if expr != "" {
			alias := cc.Alias
			if alias == "" {
				alias = cc.Label
			}
			selectCols = append(selectCols, fmt.Sprintf("%s AS `%s`", expr, alias))
		}
	}

	if len(selectCols) == 0 {
		selectCols = append(selectCols, "*")
	}

	// 3. FROM Base Table
	fromClause := fmt.Sprintf("FROM `%s` AS `%s`", ast.BaseTable.Table, ast.BaseTable.Alias)

	// 4. Joins
	var joinClauses []string
	for _, j := range ast.Joins {
		jType := "LEFT JOIN"
		switch j.Type {
		case domain.JoinInner:
			jType = "INNER JOIN"
		case domain.JoinRight:
			jType = "RIGHT JOIN"
		case domain.JoinFull:
			jType = "FULL JOIN"
		}

		onCondition := fmt.Sprintf("`%s`.`%s` = `%s`.`%s`", j.FromTable, j.FromField, j.Alias, j.ToField)
		if j.ConvertString {
			switch strings.ToUpper(j.CastMode) {
			case "FROM_ONLY":
				onCondition = fmt.Sprintf("CAST(`%s`.`%s` AS CHAR) = `%s`.`%s`", j.FromTable, j.FromField, j.Alias, j.ToField)
			case "TO_ONLY":
				onCondition = fmt.Sprintf("`%s`.`%s` = CAST(`%s`.`%s` AS CHAR)", j.FromTable, j.FromField, j.Alias, j.ToField)
			default:
				onCondition = fmt.Sprintf("CAST(`%s`.`%s` AS CHAR) = CAST(`%s`.`%s` AS CHAR)", j.FromTable, j.FromField, j.Alias, j.ToField)
			}
		}

		// Join filter applied directly to the ON clause
		if len(j.JoinFilter) > 0 {
			var filterParts []string
			for k, v := range j.JoinFilter {
				filterParts = append(filterParts, fmt.Sprintf("`%s`.`%s` = '%v'", j.Alias, k, v))
			}
			onCondition += " AND " + strings.Join(filterParts, " AND ")
		}

		joinClauses = append(joinClauses, fmt.Sprintf("%s `%s` AS `%s` ON %s", jType, j.ToTable, j.Alias, onCondition))
	}

	// 5. WHERE Clauses
	var whereClauses []string
	if len(ast.BaseTable.Filter) > 0 {
		for k, v := range ast.BaseTable.Filter {
			whereClauses = append(whereClauses, fmt.Sprintf("`%s`.`%s` = '%v'", ast.BaseTable.Alias, k, v))
		}
	}

	for _, cond := range ast.WhereFilters {
		if cond.IsParamRef {
			if isRoutine {
				whereClauses = append(whereClauses, fmt.Sprintf("(p_%s IS NULL OR `%s`.`%s` = p_%s)", cond.ParamName, cond.Table, cond.Column, cond.ParamName))
			} else if parameterized {
				whereClauses = append(whereClauses, fmt.Sprintf("(? IS NULL OR `%s`.`%s` = ?)", cond.Table, cond.Column))
			} else {
				foundDefault := false
				for _, p := range ast.Parameters {
					if strings.EqualFold(p.ParamName, cond.ParamName) {
						val := p.Paramvalue
						if val == nil || val == "" {
							val = p.DefaultValue
						}
						if val != nil {
							whereClauses = append(whereClauses, fmt.Sprintf("`%s`.`%s` = '%v'", cond.Table, cond.Column, val))
							foundDefault = true
							break
						}
					}
				}
				if !foundDefault {
					whereClauses = append(whereClauses, fmt.Sprintf("`%s`.`%s` = '%v'", cond.Table, cond.Column, cond.Value))
				}
			}
		} else if cond.Value != nil {
			whereClauses = append(whereClauses, fmt.Sprintf("`%s`.`%s` = '%v'", cond.Table, cond.Column, cond.Value))
		}
	}

	// 6. GROUP BY
	var groupByCols []string
	for _, g := range ast.GroupBy {
		groupByCols = append(groupByCols, fmt.Sprintf("`%s`.`%s`", g.Table, g.Field))
	}

	// Build full SQL
	sql := fmt.Sprintf("SELECT\n  %s\n%s", strings.Join(selectCols, ",\n  "), fromClause)
	if len(joinClauses) > 0 {
		sql += "\n" + strings.Join(joinClauses, "\n")
	}
	if len(whereClauses) > 0 {
		sql += "\nWHERE " + strings.Join(whereClauses, " AND ")
	}
	if len(groupByCols) > 0 {
		sql += "\nGROUP BY " + strings.Join(groupByCols, ", ")
	}

	return sql + ";"
}

// renderMySQLFunctionExpression renders template strings by inserting positional and collective arguments.
//
// Purpose:
// Replaces {{0}}, {{1}}, and {{args}} template tokens with formatted MySQL SQL expressions.
//
// Where it is used:
// Used in buildMySQLFunctionExpression when an expression formula template is provided.
//
// When can it be used:
// Can be used whenever an aggregate or custom column uses a custom template pattern in MySQL.
func renderMySQLFunctionExpression(template string, operands []planner.ASTOperand) string {
	expr := template
	var allArgs []string
	for i, op := range operands {
		opSQL := formatMySQLOperand(op)
		expr = strings.ReplaceAll(expr, fmt.Sprintf("{{%d}}", i), opSQL)
		allArgs = append(allArgs, opSQL)
	}
	return strings.ReplaceAll(expr, "{{args}}", strings.Join(allArgs, ", "))
}

// buildMySQLFunctionExpression maps abstract function names into MySQL SQL expressions.
//
// Purpose:
// Translates calculations, string manipulations, date operations, and aggregations into valid MySQL SQL syntax.
//
// Where it is used:
// Used in buildSelectSQL when compiling SELECT projections, calculations, and aggregations.
//
// When can it be used:
// Can be used whenever compiling calculated or aggregated columns for MySQL.
func buildMySQLFunctionExpression(fnName string, operands []planner.ASTOperand) string {
	fn := strings.ToUpper(strings.TrimSpace(fnName))
	first := "*"
	if len(operands) > 0 {
		first = formatMySQLOperand(operands[0])
	}
	switch fn {
	case "COUNT_ALL", "COUNT(*)":
		return "COUNT(*)"
	case "COUNT":
		if first == "" {
			first = "*"
		}
		return fmt.Sprintf("COUNT(%s)", first)
	case "COUNT_DISTINCT":
		return fmt.Sprintf("COUNT(DISTINCT %s)", first)
	case "COUNT_IF":
		return fmt.Sprintf("COUNT(CASE WHEN %s THEN 1 END)", first)
	case "SUM_IF":
		if len(operands) >= 2 {
			return fmt.Sprintf("SUM(CASE WHEN %s THEN %s ELSE 0 END)", first, formatMySQLOperand(operands[1]))
		}
		return fmt.Sprintf("SUM(CASE WHEN %s THEN 1 ELSE 0 END)", first)
	case "SUM", "AVG", "MIN", "MAX", "ABS", "SQRT":
		return fmt.Sprintf("%s(%s)", fn, first)
	case "ADD":
		return buildMySQLBinaryExpression(operands, "+")
	case "SUBTRACT":
		return buildMySQLBinaryExpression(operands, "-")
	case "MULTIPLY":
		return buildMySQLBinaryExpression(operands, "*")
	case "DIVIDE":
		if len(operands) < 2 {
			return ""
		}
		return fmt.Sprintf("(%s / NULLIF(%s, 0))", formatMySQLOperand(operands[0]), formatMySQLOperand(operands[1]))
	case "MODULO", "MOD":
		if len(operands) < 2 {
			return ""
		}
		return fmt.Sprintf("MOD(%s, %s)", formatMySQLOperand(operands[0]), formatMySQLOperand(operands[1]))
	case "POWER", "POW":
		if len(operands) < 2 {
			return ""
		}
		return fmt.Sprintf("POW(%s, %s)", formatMySQLOperand(operands[0]), formatMySQLOperand(operands[1]))
	case "ROUND":
		if len(operands) >= 2 {
			return fmt.Sprintf("ROUND(%s, %s)", first, formatMySQLOperand(operands[1]))
		}
		return fmt.Sprintf("ROUND(%s)", first)
	case "CEIL", "CEILING":
		return fmt.Sprintf("CEIL(%s)", first)
	case "FLOOR":
		return fmt.Sprintf("FLOOR(%s)", first)
	case "CONCAT":
		return fmt.Sprintf("CONCAT(%s)", strings.Join(formatMySQLOperands(operands), ", "))
	case "CONCAT_WS":
		args := formatMySQLOperands(operands)
		if len(args) == 0 {
			return ""
		}
		return fmt.Sprintf("CONCAT_WS(%s)", strings.Join(args, ", "))
	case "UPPER", "LOWER", "TRIM", "LENGTH":
		return fmt.Sprintf("%s(%s)", fn, first)
	case "SUBSTRING":
		args := formatMySQLOperands(operands)
		if len(args) < 2 {
			return ""
		}
		return fmt.Sprintf("SUBSTRING(%s)", strings.Join(args, ", "))
	case "REPLACE":
		args := formatMySQLOperands(operands)
		if len(args) < 3 {
			return ""
		}
		return fmt.Sprintf("REPLACE(%s, %s, %s)", args[0], args[1], args[2])
	case "YEAR", "MONTH", "DAY":
		return fmt.Sprintf("%s(%s)", fn, first)
	case "NOW":
		return "NOW()"
	case "CURRENT_DATE":
		return "CURDATE()"
	case "DATE_ADD":
		if len(operands) < 2 {
			return ""
		}
		unit := "DAY"
		if len(operands) >= 3 {
			unit = strings.ToUpper(strings.Trim(formatMySQLOperand(operands[2]), "'\"`"))
		}
		return fmt.Sprintf("DATE_ADD(%s, INTERVAL %s %s)", first, formatMySQLOperand(operands[1]), unit)
	case "DATE_DIFF", "DATEDIFF":
		if len(operands) < 2 {
			return ""
		}
		return fmt.Sprintf("DATEDIFF(%s, %s)", first, formatMySQLOperand(operands[1]))
	case "EQUAL":
		if len(operands) < 2 {
			return ""
		}
		return fmt.Sprintf("(%s = %s)", formatMySQLOperand(operands[0]), formatMySQLOperand(operands[1]))
	case "NOT_EQUAL":
		if len(operands) < 2 {
			return ""
		}
		return fmt.Sprintf("(%s != %s)", formatMySQLOperand(operands[0]), formatMySQLOperand(operands[1]))
	case "GREATER_THAN":
		if len(operands) < 2 {
			return ""
		}
		return fmt.Sprintf("(%s > %s)", formatMySQLOperand(operands[0]), formatMySQLOperand(operands[1]))
	case "LESS_THAN":
		if len(operands) < 2 {
			return ""
		}
		return fmt.Sprintf("(%s < %s)", formatMySQLOperand(operands[0]), formatMySQLOperand(operands[1]))
	case "COALESCE":
		args := formatMySQLOperands(operands)
		return fmt.Sprintf("COALESCE(%s)", strings.Join(args, ", "))
	case "CASE", "IF":
		if len(operands) >= 3 {
			return fmt.Sprintf("IF(%s, %s, %s)", formatMySQLOperand(operands[0]), formatMySQLOperand(operands[1]), formatMySQLOperand(operands[2]))
		} else if len(operands) == 2 {
			return fmt.Sprintf("CASE WHEN %s THEN %s END", formatMySQLOperand(operands[0]), formatMySQLOperand(operands[1]))
		}
		return ""
	case "TO_STRING":
		return fmt.Sprintf("CAST(%s AS CHAR)", first)
	case "TO_INTEGER":
		return fmt.Sprintf("CAST(%s AS SIGNED)", first)
	case "TO_DECIMAL":
		return fmt.Sprintf("CAST(%s AS DECIMAL(18,4))", first)
	case "PERCENTAGE":
		if len(operands) < 2 {
			return ""
		}
		return fmt.Sprintf("((%s / NULLIF(%s, 0)) * 100.0)", formatMySQLOperand(operands[0]), formatMySQLOperand(operands[1]))
	case "DISCOUNT":
		if len(operands) < 2 {
			return ""
		}
		return fmt.Sprintf("(%s - (%s * (%s / 100.0)))", formatMySQLOperand(operands[0]), formatMySQLOperand(operands[0]), formatMySQLOperand(operands[1]))
	default:
		return ""
	}
}

// buildMySQLBinaryExpression builds a binary arithmetic expression for MySQL.
//
// Purpose:
// Combines two operands with an arithmetic operator inside parentheses for MySQL SQL queries.
//
// Where it is used:
// Used in buildMySQLFunctionExpression for ADD, SUBTRACT, MULTIPLY, and DIVIDE operations.
//
// When can it be used:
// Can be used whenever an infix binary arithmetic operation is compiled for MySQL.
func buildMySQLBinaryExpression(operands []planner.ASTOperand, op string) string {
	if len(operands) < 2 {
		return ""
	}
	return fmt.Sprintf("(%s %s %s)", formatMySQLOperand(operands[0]), op, formatMySQLOperand(operands[1]))
}

// formatMySQLOperands formats a slice of ASTOperands into MySQL SQL strings.
//
// Purpose:
// Converts multiple abstract operands into their formatted MySQL string representations.
//
// Where it is used:
// In buildMySQLFunctionExpression when formatting argument lists for variadic functions like CONCAT and COALESCE.
//
// When can it be used:
// Can be used whenever a function requires an array of formatted operands.
func formatMySQLOperands(operands []planner.ASTOperand) []string {
	args := make([]string, 0, len(operands))
	for _, op := range operands {
		args = append(args, formatMySQLOperand(op))
	}
	return args
}

// formatMySQLOperand formats an individual ASTOperand into a backtick-quoted column or literal.
//
// Purpose:
// Formats an operand as an identifier (`table`.`field`) or a properly escaped SQL literal string/number.
//
// Where it is used:
// Used throughout expression and projection compilation in MySQL dataset compiler.
//
// When can it be used:
// Can be used whenever an individual operand must be emitted into a MySQL SQL statement.
func formatMySQLOperand(op planner.ASTOperand) string {
	if op.SourceTable == "" || op.SourceTable == "_LITERAL_" || op.SourceTable == "CALC" {
		valStr := fmt.Sprintf("%v", op.LiteralVal)
		if valStr == "" && op.SourceField != "" {
			valStr = op.SourceField
		}
		if op.IsLiteral || op.SourceTable == "_LITERAL_" {
			if isNumericString(valStr) || strings.HasPrefix(valStr, "'") || strings.EqualFold(valStr, "TRUE") || strings.EqualFold(valStr, "FALSE") || strings.EqualFold(valStr, "NULL") {
				return valStr
			}
			return fmt.Sprintf("'%s'", strings.ReplaceAll(valStr, "'", "''"))
		}
		return fmt.Sprintf("`%s`", op.SourceField)
	}
	return fmt.Sprintf("`%s`.`%s`", op.SourceTable, op.SourceField)
}

// isNumericString checks whether a string can be parsed as a floating-point or integer number.
//
// Purpose:
// Tests whether a string represents a valid numeric value to determine quoting requirements.
//
// Where it is used:
// In formatMySQLOperand to determine whether a literal value requires quotation marks.
//
// When can it be used:
// Can be used whenever determining if a literal string is numeric in SQL.
func isNumericString(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

// buildDDL generates MySQL routine statements for PROCEDURE or FUNCTION save modes.
//
// Purpose:
// Generates CREATE PROCEDURE or CREATE FUNCTION statements to persist the dataset query logic inside MySQL.
//
// Where it is used:
// Used in Compile when SaveMode is SaveModeProcedure or SaveModeFunction.
//
// When can it be used:
// Can be used whenever persisting a dataset query as a callable MySQL stored routine.
func (c *MySQLDataSetCompiler) buildDDL(procName, querySQL string, params []domain.FilterParam, mode domain.SaveMode) string {
	if mode == domain.SaveModeQuery {
		return ""
	}

	cleanName := strings.ReplaceAll(procName, "-", "_")
	var paramDefs []string
	for _, p := range params {
		myType := "VARCHAR(255)"
		switch strings.ToLower(p.ParamDataType) {
		case "int", "integer":
			myType = "INT"
		case "decimal", "numeric", "float":
			myType = "DECIMAL(18,4)"
		case "boolean", "bool":
			myType = "TINYINT(1)"
		case "date":
			myType = "DATE"
		case "timestamp", "datetime":
			myType = "DATETIME"
		}
		paramDefs = append(paramDefs, fmt.Sprintf("IN p_%s %s", p.ParamName, myType))
	}

	if mode == domain.SaveModeFunction {
		return fmt.Sprintf(`DROP FUNCTION IF EXISTS fn_%s;
CREATE FUNCTION fn_%s(%s)
RETURNS JSON
READS SQL DATA
DETERMINISTIC
BEGIN
    DECLARE result_json JSON;
    -- Executable query for dataset '%s'
    RETURN (SELECT JSON_ARRAYAGG(JSON_OBJECT('row', t)) FROM (%s) t);
END;`, cleanName, cleanName, strings.Join(paramDefs, ", "), procName, strings.TrimSuffix(querySQL, ";"))
	}

	return fmt.Sprintf(`DROP PROCEDURE IF EXISTS sp_%s;
CREATE PROCEDURE sp_%s(%s)
BEGIN
    -- Executable query for dataset '%s'
    %s
END;`, cleanName, cleanName, strings.Join(paramDefs, ", "), procName, querySQL)
}

// CompileDataSet compiles QueryAST into MySQL SQL.
//
// Purpose:
// Compiles a QueryAST and DataSet definition into a MySQL-specific CompiledPipeline.
//
// Where it is used:
// Invoked directly on MySQLAdapter or by external callers requiring MySQL SQL compilation.
//
// When can it be used:
// Can be used whenever an application holds a MySQLAdapter instance and needs to compile a dataset.
func (a *MySQLAdapter) CompileDataSet(ctx context.Context, ast *planner.QueryAST, ds *domain.DataSet) (*compiler.CompiledPipeline, error) {
	return NewMySQLDataSetCompiler().Compile(ctx, ast, ds)
}

// DataSetCompiler returns the adapter.DataSetCompiler instance.
//
// Purpose:
// Returns the generic adapter.DataSetCompiler interface wrapper for registering with DataSetService.
//
// Where it is used:
// In application bootstrap and dependency injection (e.g. service.RegisterCompiler("mysql", myAdapter.DataSetCompiler())).
//
// When can it be used:
// Can be used when initializing the dataset engine and registering the MySQL adapter compiler.
func (a *MySQLAdapter) DataSetCompiler() adapter.DataSetCompiler {
	return &genericCompilerWrapper{c: NewMySQLDataSetCompiler()}
}

type genericCompilerWrapper struct {
	c compiler.DataSetCompiler
}

// Compile adapts generic untyped arguments to typed AST and DataSet compiler calls.
//
// Purpose:
// Unpacks generic any interface values into *planner.QueryAST and *domain.DataSet and delegates to the underlying compiler.
//
// Where it is used:
// Called dynamically by DataSetService.Preview and DataSetService.Save via the adapter.DataSetCompiler interface.
//
// When can it be used:
// Can be used whenever compiling across module boundaries where interface{} decoupling is employed.
func (w *genericCompilerWrapper) Compile(ctx context.Context, ast any, ds any) (any, error) {
	qAst, _ := ast.(*planner.QueryAST)
	dSet, _ := ds.(*domain.DataSet)
	return w.c.Compile(ctx, qAst, dSet)
}
