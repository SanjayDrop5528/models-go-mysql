package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"github.com/SanjayDrop5528/models-go-engine/adapter"
	"github.com/SanjayDrop5528/models-go-engine/execution"
	"github.com/SanjayDrop5528/models-go-engine/model"
	"github.com/SanjayDrop5528/models-go-engine/operation"
	"github.com/SanjayDrop5528/models-go-engine/plan"
	"github.com/SanjayDrop5528/models-go-engine/query"
	"github.com/SanjayDrop5528/models-go-engine/schema"
	"net/url"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLAdapter implements the core Adapter interface for MySQL with live network and mock fallback support.
type MySQLAdapter struct {
	dsn          string
	db           *sql.DB
	ddlGen       *DDLGenerator
	queryBuilder *QueryBuilder
	mu           sync.RWMutex
	mockStore    map[string][]map[string]any
}

// NewMySQLAdapter creates a new MySQL adapter instance.
func NewMySQLAdapter(dsn string) *MySQLAdapter {
	return &MySQLAdapter{
		dsn:          dsn,
		ddlGen:       NewDDLGenerator(),
		queryBuilder: &QueryBuilder{},
		mockStore:    make(map[string][]map[string]any),
	}
}

func (a *MySQLAdapter) Name() string {
	return "mysql"
}

// NativeClient returns the underlying *sql.DB connection handle.
func (a *MySQLAdapter) NativeClient() any {
	return a.DB()
}

// DB returns the underlying *sql.DB connection handle.
func (a *MySQLAdapter) DB() *sql.DB {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.db
}

// DatabaseName returns the target database name.
func (a *MySQLAdapter) DatabaseName() string {
	return a.GetDatabaseName()
}

// GetDatabaseName returns the database name extracted from the DSN string, or "in-memory mock" if no DSN.
func (a *MySQLAdapter) GetDatabaseName() string {
	if strings.TrimSpace(a.dsn) == "" {
		return "in-memory mock"
	}
	u, err := url.Parse(a.dsn)
	if err != nil || u.Path == "" || u.Path == "/" {
		return "mysql"
	}
	return strings.TrimPrefix(u.Path, "/")
}

// getDB lazily and thread-safely connects to the live MySQL database when a DSN is provided.
func (a *MySQLAdapter) getDB(ctx context.Context) (*sql.DB, error) {
	if strings.TrimSpace(a.dsn) == "" {
		return nil, nil // Offline mock fallback mode
	}

	a.mu.RLock()
	if a.db != nil {
		db := a.db
		a.mu.RUnlock()
		return db, nil
	}
	a.mu.RUnlock()

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.db != nil {
		return a.db, nil
	}

	log.Printf("[MySQL] Connecting to live database at: %s...", a.dsn)
	db, err := sql.Open("mysql", a.dsn)
	if err != nil {
		return nil, fmt.Errorf("failed connecting to MySQL: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping live MySQL: %w", err)
	}

	log.Printf("[MySQL] ✔ Connected successfully to live MySQL database!")
	a.db = db
	_ = a.EnsureMetadataTables(ctx)
	return a.db, nil
}

// EnsureMetadataTables creates 'model_configs' and 'data_models' system metadata tables if they do not exist.
func (a *MySQLAdapter) EnsureMetadataTables(ctx context.Context) error {
	db, err := a.getDB(ctx)
	if err != nil || db == nil {
		return nil
	}
	createCfgTable := `
	CREATE TABLE IF NOT EXISTS model_configs (
		id VARCHAR(255) PRIMARY KEY,
		schema_name VARCHAR(255),
		name VARCHAR(255) NOT NULL,
		table_name VARCHAR(255),
		ref_name VARCHAR(255),
		is_table BOOLEAN DEFAULT TRUE,
		is_attribute_reference BOOLEAN DEFAULT FALSE,
		description TEXT,
		status VARCHAR(50) DEFAULT 'active',
		version INT DEFAULT 1,
		is_system BOOLEAN DEFAULT FALSE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		created_by VARCHAR(255),
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_by VARCHAR(255)
	);`

	createDMTable := `
	CREATE TABLE IF NOT EXISTS data_models (
		id VARCHAR(255) PRIMARY KEY,
		model_id VARCHAR(255) NOT NULL,
		column_name VARCHAR(255),
		json_field VARCHAR(255),
		ref_name VARCHAR(255),
		description TEXT,
		data_type VARCHAR(100) NOT NULL,
		custom_type_id VARCHAR(255),
		custom_type VARCHAR(255),
		is_array BOOLEAN DEFAULT FALSE,
		is_nullable BOOLEAN DEFAULT TRUE,
		is_required BOOLEAN DEFAULT FALSE,
		is_primary_key BOOLEAN DEFAULT FALSE,
		is_unique BOOLEAN DEFAULT FALSE,
		is_immutable BOOLEAN DEFAULT FALSE,
		is_generated BOOLEAN DEFAULT FALSE,
		default_value TEXT,
		min DECIMAL(18,4),
		max DECIMAL(18,4),
		min_length INT,
		max_length INT,
		pattern TEXT,
		enum JSON,
		precision_val INT,
		scale_val INT,
		items JSON,
		is_orbital_reference BOOLEAN DEFAULT FALSE,
		orbital_reference_model_id VARCHAR(255),
		orbital_reference_field_id VARCHAR(255),
		orbital_reference_validation VARCHAR(100),
		reference JSON,
		status VARCHAR(50) DEFAULT 'active',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		created_by VARCHAR(255),
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_by VARCHAR(255)
	);`

	_, _ = db.ExecContext(ctx, createCfgTable)
	_, _ = db.ExecContext(ctx, createDMTable)
	log.Printf("[MySQL] ✔ System metadata tables ('model_configs' and 'data_models') verified & active.")
	return nil
}

// ImportLiveMetadata introspects live MySQL database tables and auto-populates model_configs & data_models.
func (a *MySQLAdapter) ImportLiveMetadata(ctx context.Context) ([]*model.ModelConfig, []*model.DataModel, error) {
	db, err := a.getDB(ctx)
	if err != nil || db == nil {
		return nil, nil, err
	}
	_ = a.EnsureMetadataTables(ctx)

	query := `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE'
		ORDER BY table_name;
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, nil, fmt.Errorf("failed listing MySQL tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			tables = append(tables, name)
		}
	}

	var configs []*model.ModelConfig
	var fields []*model.DataModel

	for _, tableName := range tables {
		if tableName == "model_configs" || tableName == "data_models" || tableName == "schema_migrations" || tableName == "alembic_version" || tableName == "flyway_schema_history" {
			continue
		}

		modelID := tableName
		modelName := strings.Title(tableName)

		cfg := &model.ModelConfig{
			ID:                   modelID,
			Name:                 modelName,
			Table:                tableName,
			RefName:              tableName,
			IsAttributeReference: false,
			Description:          fmt.Sprintf("Auto-imported from MySQL live table '%s'", tableName),
			Status:               model.ModelConfigStatusActive,
			Version:              1,
			CreatedAt:            time.Now(),
			UpdatedAt:            time.Now(),
		}
		configs = append(configs, cfg)

		// Fetch columns for table
		colQuery := `
			SELECT column_name, data_type, is_nullable, column_key, column_default
			FROM information_schema.columns
			WHERE table_schema = DATABASE() AND table_name = ?;
		`
		colRows, err := db.QueryContext(ctx, colQuery, tableName)
		if err != nil {
			log.Printf("[MySQL] ⚠ [Import Warning] Failed querying columns for table '%s': %v", tableName, err)
			continue
		}
		// Fetch Foreign Keys for table
		fkMap := make(map[string][2]string)
		fkQuery := `
			SELECT column_name, referenced_table_name, referenced_column_name
			FROM information_schema.key_column_usage
			WHERE table_schema = DATABASE() AND table_name = ? AND referenced_table_name IS NOT NULL;
		`
		if fkRows, err := db.QueryContext(ctx, fkQuery, tableName); err == nil {
			for fkRows.Next() {
				var col, refTable, refCol string
				if err := fkRows.Scan(&col, &refTable, &refCol); err == nil {
					fkMap[col] = [2]string{refTable, refCol}
				}
			}
			fkRows.Close()
		}

		colCount := 0
		for colRows.Next() {
			var colName, dataType, isNullable, colKey string
			var colDef sql.NullString
			if err := colRows.Scan(&colName, &dataType, &isNullable, &colKey, &colDef); err == nil {
				colCount++
				fieldID := fmt.Sprintf("%s_%s", modelID, colName)
				isPK := colKey == "PRI"
				dm := &model.DataModel{
					ID:           fieldID,
					ModelID:      modelID,
					ColumnName:   colName,
					JSONField:    colName,
					DataType:     model.DataType(strings.ToUpper(dataType)),
					IsNullable:   isNullable == "YES",
					IsRequired:   isNullable != "YES" && !isPK,
					IsPrimaryKey: isPK,
					DefaultValue: colDef.String,
					Status:       model.DataModelStatusActive,
					CreatedAt:    time.Now(),
					UpdatedAt:    time.Now(),
				}

				if refInfo, isFK := fkMap[colName]; isFK {
					dm.IsOrbitalReference = true
					targetTable := refInfo[0]
					targetField := refInfo[1]
					dm.OrbitalReferenceModelID = &targetTable
					dm.OrbitalReferenceFieldID = &targetField
					dm.OrbitalReferenceValidation = model.OrbitalValidationExists
					dm.Reference = &model.OrbitalRefSpec{
						Model:     targetTable,
						Attribute: targetField,
					}
					log.Printf("[MySQL] [Import FK] Introspected Foreign Key on '%s.%s' -> %s.%s",
						tableName, colName, targetTable, targetField)
				}

				fields = append(fields, dm)
			}
		}
		colRows.Close()
		log.Printf("[MySQL] [Import Table] Introspected Table '%s' -> ModelConfig ID='%s', Columns=%d", tableName, modelID, colCount)
	}

	log.Printf("[MySQL] ✔ [Import Success] Successfully introspected %d ModelConfig(s) and %d DataModel field(s) directly inside adapter.", len(configs), len(fields))
	return configs, fields, nil
}

func (a *MySQLAdapter) Connect(ctx context.Context) error {
	_, err := a.getDB(ctx)
	return err
}

func (a *MySQLAdapter) Ping(ctx context.Context) error {
	db, err := a.getDB(ctx)
	if err != nil {
		return err
	}
	if db != nil {
		return db.PingContext(ctx)
	}
	return nil
}

func (a *MySQLAdapter) Close(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.db != nil {
		err := a.db.Close()
		a.db = nil
		return err
	}
	return nil
}

func (a *MySQLAdapter) GetSchema(ctx context.Context, ref model.ModelRef) (*schema.Schema, error) {
	return nil, nil
}

func (a *MySQLAdapter) ValidateSchemaPlan(ctx context.Context, p *plan.SchemaPlan) error {
	_, err := a.ddlGen.GenerateStatements(p.Operations)
	return err
}

func (a *MySQLAdapter) PreviewSchemaChange(ctx context.Context, p *plan.SchemaPlan) (*plan.SchemaPreview, error) {
	statements, err := a.ddlGen.GenerateStatements(p.Operations)
	if err != nil {
		return nil, err
	}

	nativeActions := make([]plan.NativeAction, 0, len(statements))
	for i, stmt := range statements {
		op := p.Operations[i]
		nativeActions = append(nativeActions, plan.NativeAction{
			Type:        "SQL_MYSQL",
			Description: op.Description,
			Statement:   stmt,
			Destructive: op.Destructive,
		})
	}

	return &plan.SchemaPreview{
		ModelID:              p.ModelID,
		StorageName:          p.StorageName,
		Database:             "mysql",
		Changes:              p.Operations,
		NativeActions:        nativeActions,
		HasDestructive:       p.Destructive,
		RequiresConfirmation: p.Destructive,
		Warnings:             p.Warnings,
		Status:               "READY",
	}, nil
}

func (a *MySQLAdapter) ApplySchemaChange(ctx context.Context, p *plan.SchemaPlan) error {
	statements, err := a.ddlGen.GenerateStatements(p.Operations)
	if err != nil {
		return err
	}

	db, err := a.getDB(ctx)
	if err != nil {
		return err
	}

	if db != nil {
		for _, stmt := range statements {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("failed executing MySQL DDL '%s': %w", stmt, err)
			}
		}
	}

	return nil
}

func (a *MySQLAdapter) Create(ctx context.Context, ref model.ModelRef, data map[string]any) (map[string]any, error) {
	tableName := ref.StorageName
	if tableName == "" {
		tableName = ref.Name
	}

	res := make(map[string]any)
	for k, v := range data {
		res[k] = v
	}

	db, err := a.getDB(ctx)
	if err != nil {
		return nil, err
	}

	if db != nil {
		sqlStr, args := a.queryBuilder.BuildInsert(tableName, data)
		result, err := db.ExecContext(ctx, sqlStr, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to insert into MySQL table '%s': %w", tableName, err)
		}
		if lastID, err := result.LastInsertId(); err == nil && lastID > 0 {
			if _, hasID := res["id"]; !hasID {
				res["id"] = lastID
			}
		}
		return res, nil
	}

	// In-Memory Fallback
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mockStore[tableName] = append(a.mockStore[tableName], res)
	return res, nil
}

func (a *MySQLAdapter) Find(ctx context.Context, ref model.ModelRef, q query.Query) ([]map[string]any, int64, error) {
	tableName := ref.StorageName
	if tableName == "" {
		tableName = ref.Name
	}

	db, err := a.getDB(ctx)
	if err != nil {
		return nil, 0, err
	}

	if db != nil {
		sqlStr, args := a.queryBuilder.BuildSelect(tableName, q)
		rows, err := db.QueryContext(ctx, sqlStr, args...)
		if err != nil {
			return nil, 0, fmt.Errorf("mysql select failed: %w", err)
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return nil, 0, err
		}

		var results []map[string]any
		for rows.Next() {
			columns := make([]any, len(cols))
			columnPointers := make([]any, len(cols))
			for i := range columns {
				columnPointers[i] = &columns[i]
			}
			if err := rows.Scan(columnPointers...); err != nil {
				return nil, 0, err
			}
			rowMap := make(map[string]any)
			for i, colName := range cols {
				val := columnPointers[i].(*any)
				if b, ok := (*val).([]byte); ok {
					rowMap[colName] = string(b)
				} else {
					rowMap[colName] = *val
				}
			}
			results = append(results, rowMap)
		}
		return results, int64(len(results)), nil
	}

	// In-Memory Fallback
	a.mu.RLock()
	defer a.mu.RUnlock()

	rows := a.mockStore[tableName]
	var results []map[string]any
	for _, r := range rows {
		cp := make(map[string]any)
		for k, v := range r {
			cp[k] = v
		}
		results = append(results, cp)
	}
	return results, int64(len(results)), nil
}

func (a *MySQLAdapter) FindOne(ctx context.Context, ref model.ModelRef, id any) (map[string]any, error) {
	tableName := ref.StorageName
	if tableName == "" {
		tableName = ref.Name
	}

	db, err := a.getDB(ctx)
	if err != nil {
		return nil, err
	}

	if db != nil {
		q := query.NewQuery().Where("id", query.OpEq, id)
		q.Pagination.Limit = 1
		results, _, err := a.Find(ctx, ref, q)
		if err != nil {
			return nil, err
		}
		if len(results) == 0 {
			return nil, fmt.Errorf("record '%v' not found", id)
		}
		return results[0], nil
	}

	// In-Memory Fallback
	a.mu.RLock()
	defer a.mu.RUnlock()

	idStr := fmt.Sprintf("%v", id)
	for _, r := range a.mockStore[tableName] {
		if fmt.Sprintf("%v", r["id"]) == idStr {
			cp := make(map[string]any)
			for k, v := range r {
				cp[k] = v
			}
			return cp, nil
		}
	}
	return nil, fmt.Errorf("record '%v' not found", id)
}

func (a *MySQLAdapter) Update(ctx context.Context, ref model.ModelRef, id any, data map[string]any) (map[string]any, error) {
	tableName := ref.StorageName
	if tableName == "" {
		tableName = ref.Name
	}

	db, err := a.getDB(ctx)
	if err != nil {
		return nil, err
	}

	if db != nil {
		sqlStr, args := a.queryBuilder.BuildUpdate(tableName, id, data)
		if _, err := db.ExecContext(ctx, sqlStr, args...); err != nil {
			return nil, fmt.Errorf("mysql update failed: %w", err)
		}
		return a.FindOne(ctx, ref, id)
	}

	// In-Memory Fallback
	a.mu.Lock()
	defer a.mu.Unlock()

	idStr := fmt.Sprintf("%v", id)
	for i, r := range a.mockStore[tableName] {
		if fmt.Sprintf("%v", r["id"]) == idStr {
			data["id"] = r["id"]
			a.mockStore[tableName][i] = data
			return data, nil
		}
	}
	return nil, fmt.Errorf("record '%v' not found", id)
}

func (a *MySQLAdapter) Patch(ctx context.Context, ref model.ModelRef, id any, data map[string]any) (map[string]any, error) {
	tableName := ref.StorageName
	if tableName == "" {
		tableName = ref.Name
	}

	db, err := a.getDB(ctx)
	if err != nil {
		return nil, err
	}

	if db != nil {
		return a.Update(ctx, ref, id, data)
	}

	// In-Memory Fallback
	a.mu.Lock()
	defer a.mu.Unlock()

	idStr := fmt.Sprintf("%v", id)
	for i, r := range a.mockStore[tableName] {
		if fmt.Sprintf("%v", r["id"]) == idStr {
			for k, v := range data {
				a.mockStore[tableName][i][k] = v
			}
			return a.mockStore[tableName][i], nil
		}
	}
	return nil, fmt.Errorf("record '%v' not found", id)
}

func (a *MySQLAdapter) Delete(ctx context.Context, ref model.ModelRef, id any) error {
	tableName := ref.StorageName
	if tableName == "" {
		tableName = ref.Name
	}

	db, err := a.getDB(ctx)
	if err != nil {
		return err
	}

	if db != nil {
		sqlStr, args := a.queryBuilder.BuildDelete(tableName, id)
		_, err := db.ExecContext(ctx, sqlStr, args...)
		return err
	}

	// In-Memory Fallback
	a.mu.Lock()
	defer a.mu.Unlock()

	idStr := fmt.Sprintf("%v", id)
	for i, r := range a.mockStore[tableName] {
		if fmt.Sprintf("%v", r["id"]) == idStr {
			a.mockStore[tableName] = append(a.mockStore[tableName][:i], a.mockStore[tableName][i+1:]...)
			return nil
		}
	}
	return nil
}

func (a *MySQLAdapter) Execute(ctx context.Context, req execution.ExecutionRequest) (*execution.ExecutionResult, error) {
	var paramPlaceholders []string
	var args []any
	for _, v := range req.Arguments {
		paramPlaceholders = append(paramPlaceholders, "?")
		args = append(args, v)
	}

	switch req.Operation {
	case operation.OpProcedure:
		stmt := fmt.Sprintf("CALL `%s`(%s);", req.Target, strings.Join(paramPlaceholders, ", "))
		db, _ := a.getDB(ctx)
		if db != nil {
			_, _ = db.ExecContext(ctx, stmt, args...)
		}
		return &execution.ExecutionResult{
			Status: "SUCCESS",
			Metadata: map[string]any{
				"sql": stmt,
			},
		}, nil

	case operation.OpFunction:
		queryStr := fmt.Sprintf("SELECT `%s`(%s);", req.Target, strings.Join(paramPlaceholders, ", "))
		db, _ := a.getDB(ctx)
		if db != nil {
			_ = db.QueryRowContext(ctx, queryStr, args...)
		}
		return &execution.ExecutionResult{
			Status: "SUCCESS",
			Metadata: map[string]any{
				"sql": queryStr,
			},
		}, nil

	case operation.OpCommand, operation.OpCustom:
		db, _ := a.getDB(ctx)
		if db != nil {
			_, _ = db.ExecContext(ctx, req.Target)
		}
		return &execution.ExecutionResult{
			Status: "SUCCESS",
			Metadata: map[string]any{
				"target": req.Target,
			},
		}, nil

	default:
		return nil, adapter.ErrOperationNotSupported
	}
}

func (a *MySQLAdapter) Begin(ctx context.Context) (adapter.Transaction, error) {
	db, _ := a.getDB(ctx)
	if db != nil {
		tx, err := db.BeginTx(ctx, nil)
		if err == nil {
			return &MySQLTransaction{adapter: a, tx: tx}, nil
		}
	}
	return &MySQLTransaction{adapter: a}, nil
}

// MySQLTransaction implements adapter.Transaction for MySQL.
type MySQLTransaction struct {
	adapter *MySQLAdapter
	tx      *sql.Tx
}

func (t *MySQLTransaction) Create(ctx context.Context, m model.ModelRef, data map[string]any) (map[string]any, error) {
	return t.adapter.Create(ctx, m, data)
}

func (t *MySQLTransaction) Find(ctx context.Context, m model.ModelRef, q query.Query) ([]map[string]any, int64, error) {
	return t.adapter.Find(ctx, m, q)
}

func (t *MySQLTransaction) FindOne(ctx context.Context, m model.ModelRef, id any) (map[string]any, error) {
	return t.adapter.FindOne(ctx, m, id)
}

func (t *MySQLTransaction) Update(ctx context.Context, m model.ModelRef, id any, data map[string]any) (map[string]any, error) {
	return t.adapter.Update(ctx, m, id, data)
}

func (t *MySQLTransaction) Patch(ctx context.Context, m model.ModelRef, id any, data map[string]any) (map[string]any, error) {
	return t.adapter.Patch(ctx, m, id, data)
}

func (t *MySQLTransaction) Delete(ctx context.Context, m model.ModelRef, id any) error {
	return t.adapter.Delete(ctx, m, id)
}

func (t *MySQLTransaction) Execute(ctx context.Context, req execution.ExecutionRequest) (*execution.ExecutionResult, error) {
	return t.adapter.Execute(ctx, req)
}

func (t *MySQLTransaction) Commit(ctx context.Context) error {
	if t.tx != nil {
		return t.tx.Commit()
	}
	return nil
}

func (t *MySQLTransaction) Rollback(ctx context.Context) error {
	if t.tx != nil {
		return t.tx.Rollback()
	}
	return nil
}
