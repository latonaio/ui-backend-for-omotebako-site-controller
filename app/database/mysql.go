package database

import (
	"database/sql"
	"ui-backend-for-omotebako-site-controller/config"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/xerrors"
)

type Database struct {
	DB *sql.DB
}

func NewDatabase(mysqlEnv *config.MysqlEnv) (*Database, error) {
	db, err := sql.Open("mysql", mysqlEnv.DSN())
	if err != nil {
		return nil, xerrors.Errorf(`database open error: %w`, err)
	}
	if err = db.Ping(); err != nil {
		return nil, xerrors.Errorf(`failed to connection database: %w`, err)
	}
	return &Database{
		DB: db,
	}, nil
}
