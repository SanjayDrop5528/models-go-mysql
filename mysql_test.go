package mysql_test

import (
	"github.com/SanjayDrop5528/models-go-mysql"
	"github.com/SanjayDrop5528/models-go-engine/diff"
	"github.com/SanjayDrop5528/models-go-engine/model"
	"github.com/SanjayDrop5528/models-go-engine/schema"
	"testing"
)

func TestMySQL_DDL_AddColumn(t *testing.T) {
	gen := mysql.NewDDLGenerator()

	op := diff.SchemaOperation{
		Type:        diff.OpAddColumn,
		TargetTable: "employees",
		ObjectName:  "salary",
		After: schema.SchemaAttribute{
			Name:      "salary",
			Type:      model.TypeDecimal,
			Precision: 10,
			Scale:     2,
			Nullable:  true,
		},
	}

	stmt, err := gen.GenerateStatement(op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "ALTER TABLE `employees` ADD COLUMN `salary` DECIMAL(10, 2);"
	if stmt != expected {
		t.Fatalf("expected:\n%s\ngot:\n%s", expected, stmt)
	}
}

func TestMySQL_DDL_RenameColumn(t *testing.T) {
	gen := mysql.NewDDLGenerator()

	op := diff.SchemaOperation{
		Type:        diff.OpRenameColumn,
		TargetTable: "employees",
		OldName:     "employee_name",
		ObjectName:  "name",
	}

	stmt, err := gen.GenerateStatement(op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "ALTER TABLE `employees` RENAME COLUMN `employee_name` TO `name`;"
	if stmt != expected {
		t.Fatalf("expected:\n%s\ngot:\n%s", expected, stmt)
	}
}

func TestMySQL_WithSchemas(t *testing.T) {
	adapter := mysql.NewMySQLAdapter("root:password@tcp(127.0.0.1:3306)/testdb").WithSchemas("tenant_db", "sales_db")
	if adapter.Name() != "mysql" {
		t.Fatalf("expected adapter name mysql, got: %s", adapter.Name())
	}
}
