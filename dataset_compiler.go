package mysql

import (
	"context"
	"fmt"
	"strings"

	"github.com/SanjayDrop5528/models-go-engine/adapter"
	"github.com/SanjayDrop5528/models-go-engine/dataset/compiler"
	"github.com/SanjayDrop5528/models-go-engine/dataset/domain"
	"github.com/SanjayDrop5528/models-go-engine/dataset/planner"
)

// MySQLDataSetCompiler compiles QueryAST into MySQL SQL and Stored Procedures/Functions.
type MySQLDataSetCompiler struct{}

// NewMySQLDataSetCompiler creates a new MySQL dataset compiler instance.
func NewMySQLDataSetCompiler() *MySQLDataSetCompiler {
	return &MySQLDataSetCompiler{}
}

// Compile compiles the QueryAST into MySQL SQL.
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
					if strings.EqualFold(p.ParamName, cond.ParamName) && p.DefaultValue != nil {
						whereClauses = append(whereClauses, fmt.Sprintf("`%s`.`%s` = '%v'", cond.Table, cond.Column, p.DefaultValue))
						foundDefault = true
						break
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
	case "YEAR", "MONTH", "DAY":
		return fmt.Sprintf("%s(%s)", fn, first)
	case "NOW":
		return "NOW()"
	case "CURRENT_DATE":
		return "CURDATE()"
	default:
		return ""
	}
}

func buildMySQLBinaryExpression(operands []planner.ASTOperand, op string) string {
	if len(operands) < 2 {
		return ""
	}
	return fmt.Sprintf("(%s %s %s)", formatMySQLOperand(operands[0]), op, formatMySQLOperand(operands[1]))
}

func formatMySQLOperands(operands []planner.ASTOperand) []string {
	args := make([]string, 0, len(operands))
	for _, op := range operands {
		args = append(args, formatMySQLOperand(op))
	}
	return args
}

func formatMySQLOperand(op planner.ASTOperand) string {
	if op.IsLiteral || op.SourceTable == "" || op.SourceTable == "_LITERAL_" {
		return fmt.Sprintf("%v", op.LiteralVal)
	}
	return fmt.Sprintf("`%s`.`%s`", op.SourceTable, op.SourceField)
}

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
func (a *MySQLAdapter) CompileDataSet(ctx context.Context, ast *planner.QueryAST, ds *domain.DataSet) (*compiler.CompiledPipeline, error) {
	return NewMySQLDataSetCompiler().Compile(ctx, ast, ds)
}

// DataSetCompiler returns the adapter.DataSetCompiler instance.
func (a *MySQLAdapter) DataSetCompiler() adapter.DataSetCompiler {
	return &genericCompilerWrapper{c: NewMySQLDataSetCompiler()}
}

type genericCompilerWrapper struct {
	c compiler.DataSetCompiler
}

func (w *genericCompilerWrapper) Compile(ctx context.Context, ast any, ds any) (any, error) {
	qAst, _ := ast.(*planner.QueryAST)
	dSet, _ := ds.(*domain.DataSet)
	return w.c.Compile(ctx, qAst, dSet)
}
