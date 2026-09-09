package mysql

import (
	"context"

	"github.com/SanjayDrop5528/models-go-engine/rdbms"
	"github.com/SanjayDrop5528/models-go-engine/rdbms/dialect"
)

// RDBMS returns an active *rdbms.DB instance tied to this adapter's DB and MySQL dialect.
func (a *MySQLAdapter) RDBMS(ctx context.Context) (*rdbms.DB, error) {
	db, err := a.getDB(ctx)
	if err != nil {
		return nil, err
	}
	return rdbms.NewDB(db, dialect.NewMySQL()), nil
}

// NewSelect returns a new rdbms.SelectQuery configured with the MySQL dialect.
func (a *MySQLAdapter) NewSelect(ctx context.Context) (*rdbms.SelectQuery, error) {
	rdb, err := a.RDBMS(ctx)
	if err != nil {
		return nil, err
	}
	return rdb.NewSelect(), nil
}
