package mysql_test

import (
	"context"
	"strings"
	"testing"

	"github.com/SanjayDrop5528/models-go-engine/dataset/domain"
	"github.com/SanjayDrop5528/models-go-engine/dataset/planner"
	"github.com/SanjayDrop5528/models-go-engine/dataset/resolver"
	"github.com/SanjayDrop5528/models-go-engine/diff"
	"github.com/SanjayDrop5528/models-go-engine/model"
	"github.com/SanjayDrop5528/models-go-engine/schema"
	"github.com/SanjayDrop5528/models-go-mysql"
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

func TestMySQLDataSetCompiler_AggregatesAndJoinedGroupByUseAliases(t *testing.T) {
	c := mysql.NewMySQLDataSetCompiler()
	ds := &domain.DataSet{
		BaseCollection: domain.BaseCollection{Collection: "employees"},
		JoinCollections: []domain.JoinCollection{
			{
				FromCollection:      "employees",
				FromCollectionField: "department_id",
				ToCollection:        "departments",
				ToCollectionField:   "id",
				NamedAs:             "d",
				JoinType:            domain.JoinLeft,
			},
		},
		GroupByFields: []domain.GroupByField{
			{TableName: "departments", FieldName: "name"},
		},
		SelectedList: []domain.SelectedField{
			{Field: "departments.name", HeaderName: "department_name"},
		},
		Filter: map[string]any{
			"departments.is_active": true,
		},
		CustomColumns: []domain.CustomColumn{
			{
				CustomColumnName:      "total_salary",
				CustomAggregateFnName: "SUM",
				Fields: []domain.DataSetCustomField{
					{TableName: "employees", FieldName: "salary"},
				},
			},
			{
				CustomColumnName:      "distinct_departments",
				CustomAggregateFnName: "COUNT_DISTINCT",
				Fields: []domain.DataSetCustomField{
					{TableName: "d", FieldName: "id"},
				},
			},
		},
	}

	astPlanner := planner.NewPlanner(resolver.NewFunctionRegistry())
	ast, err := astPlanner.BuildAST(context.Background(), ds)
	if err != nil {
		t.Fatalf("unexpected plan error: %v", err)
	}
	res, err := c.Compile(context.Background(), ast, ds)
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	for _, want := range []string{
		"`d`.`name` AS `department_name`",
		"SUM(`employees`.`salary`) AS `total_salary`",
		"COUNT(DISTINCT `d`.`id`) AS `distinct_departments`",
		"`d`.`is_active` = 'true'",
		"GROUP BY `d`.`name`",
	} {
		if !strings.Contains(res.ExecutableQuery, want) {
			t.Fatalf("expected query to contain %s, got:\n%s", want, res.ExecutableQuery)
		}
	}
	if strings.Contains(res.ExecutableQuery, "`departments`.`name`") {
		t.Fatalf("expected joined group by field to use alias d, got:\n%s", res.ExecutableQuery)
	}
}
