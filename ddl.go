package mysql

import (
	"fmt"
	"github.com/SanjayDrop5528/models-go-engine/diff"
	"github.com/SanjayDrop5528/models-go-engine/schema"
	"strings"
)

// DDLGenerator compiles core SchemaOperations into MySQL DDL statements.
type DDLGenerator struct{}

// NewDDLGenerator creates a new MySQL DDL compiler.
func NewDDLGenerator() *DDLGenerator {
	return &DDLGenerator{}
}

// GenerateStatements transforms SchemaOperations into MySQL DDL statements.
func (g *DDLGenerator) GenerateStatements(ops []diff.SchemaOperation) ([]string, error) {
	statements := make([]string, 0, len(ops))

	for _, op := range ops {
		stmt, err := g.GenerateStatement(op)
		if err != nil {
			return nil, err
		}
		if stmt != "" {
			statements = append(statements, stmt)
		}
	}

	return statements, nil
}

// GenerateStatement compiles an individual operation for MySQL.
func (g *DDLGenerator) GenerateStatement(op diff.SchemaOperation) (string, error) {
	table := quoteIdent(op.TargetTable)

	switch op.Type {
	case diff.OpCreateTable:
		des, ok := op.After.(*schema.Schema)
		if !ok || des == nil {
			return "", fmt.Errorf("invalid schema for CREATE_TABLE")
		}
		return g.buildCreateTable(des), nil

	case diff.OpDropTable:
		return fmt.Sprintf("DROP TABLE IF EXISTS %s;", table), nil

	case diff.OpRenameTable:
		return fmt.Sprintf("RENAME TABLE %s TO %s;", table, quoteIdent(op.ObjectName)), nil

	case diff.OpAddColumn:
		attr, ok := op.After.(schema.SchemaAttribute)
		if !ok {
			return "", fmt.Errorf("invalid attribute for ADD_COLUMN")
		}
		return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", table, g.buildColumnDef(attr)), nil

	case diff.OpRemoveColumn:
		return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", table, quoteIdent(op.ObjectName)), nil

	case diff.OpRenameColumn:
		// MySQL 8.0+: RENAME COLUMN old TO new
		return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;", table, quoteIdent(op.OldName), quoteIdent(op.ObjectName)), nil

	case diff.OpAlterColumnType, diff.OpAlterColumnNullable, diff.OpAlterColumnDefault:
		if attr, ok := op.After.(schema.SchemaAttribute); ok {
			return fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s;", table, g.buildColumnDef(attr)), nil
		}
		return "", fmt.Errorf("invalid attribute for ALTER_COLUMN")

	case diff.OpAddPrimaryKey:
		pk, ok := op.After.(*schema.SchemaKey)
		if !ok || pk == nil {
			return "", fmt.Errorf("invalid PK payload")
		}
		quotedCols := make([]string, len(pk.Columns))
		for i, c := range pk.Columns {
			quotedCols[i] = quoteIdent(c)
		}
		return fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s);", table, strings.Join(quotedCols, ", ")), nil

	case diff.OpDropPrimaryKey:
		return fmt.Sprintf("ALTER TABLE %s DROP PRIMARY KEY;", table), nil

	case diff.OpAddIndex:
		idx, ok := op.After.(schema.SchemaIndex)
		if !ok {
			return "", fmt.Errorf("invalid index payload")
		}
		quotedCols := make([]string, len(idx.Columns))
		for i, c := range idx.Columns {
			quotedCols[i] = quoteIdent(c)
		}
		uniqueStr := ""
		if idx.Unique {
			uniqueStr = "UNIQUE "
		}
		return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s);", uniqueStr, quoteIdent(idx.Name), table, strings.Join(quotedCols, ", ")), nil

	case diff.OpDropIndex:
		return fmt.Sprintf("DROP INDEX %s ON %s;", quoteIdent(op.ObjectName), table), nil

	default:
		return "", fmt.Errorf("unsupported MySQL operation type: %s", op.Type)
	}
}

func (g *DDLGenerator) buildCreateTable(s *schema.Schema) string {
	lines := make([]string, 0, len(s.Attributes)+1)

	var pkCols []string
	for _, attr := range s.Attributes {
		lines = append(lines, "    "+g.buildColumnDef(attr))
		if attr.PrimaryKey && s.PrimaryKey == nil {
			pkCols = append(pkCols, attr.Name)
		}
	}

	if s.PrimaryKey != nil && len(s.PrimaryKey.Columns) > 0 {
		pkCols = s.PrimaryKey.Columns
	}

	if len(pkCols) > 0 {
		quotedPKs := make([]string, len(pkCols))
		for i, c := range pkCols {
			quotedPKs[i] = quoteIdent(c)
		}
		lines = append(lines, fmt.Sprintf("    PRIMARY KEY (%s)", strings.Join(quotedPKs, ", ")))
	}

	tableStmt := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n%s\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;", quoteIdent(s.Name), strings.Join(lines, ",\n"))

	fkStmts := g.buildForeignKeyStatements(s)
	if len(fkStmts) > 0 {
		return tableStmt + "\n" + strings.Join(fkStmts, "\n")
	}
	return tableStmt
}

func (g *DDLGenerator) buildForeignKeyStatements(s *schema.Schema) []string {
	var stmts []string
	for _, rel := range s.Relations {
		fkName := rel.Name
		if fkName == "" {
			fkName = fmt.Sprintf("fk_%s_%s", s.Name, rel.Column)
		}
		onDel := ""
		if rel.OnDelete != "" {
			onDel = fmt.Sprintf(" ON DELETE %s", rel.OnDelete)
		}
		onUpd := ""
		if rel.OnUpdate != "" {
			onUpd = fmt.Sprintf(" ON UPDATE %s", rel.OnUpdate)
		}

		addStmt := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)%s%s;",
			quoteIdent(s.Name), quoteIdent(fkName), quoteIdent(rel.Column), quoteIdent(rel.ForeignTable), quoteIdent(rel.ForeignColumn), onDel, onUpd)

		stmts = append(stmts, addStmt)
	}
	return stmts
}

func (g *DDLGenerator) buildColumnDef(attr schema.SchemaAttribute) string {
	myType := ToMySQLType(attr)
	parts := []string{quoteIdent(attr.Name), myType}

	if !attr.Nullable && !attr.AutoIncrement {
		parts = append(parts, "NOT NULL")
	}
	if attr.AutoIncrement {
		parts = append(parts, "AUTO_INCREMENT")
	}
	if attr.Default != nil {
		parts = append(parts, fmt.Sprintf("DEFAULT '%v'", attr.Default))
	}
	if attr.Unique && !attr.PrimaryKey {
		parts = append(parts, "UNIQUE")
	}

	return strings.Join(parts, " ")
}

func quoteIdent(name string) string {
	if strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)
		return fmt.Sprintf("`%s`.`%s`", strings.ReplaceAll(parts[0], "`", "``"), strings.ReplaceAll(parts[1], "`", "``"))
	}
	return fmt.Sprintf("`%s`", strings.ReplaceAll(name, "`", "``"))
}
