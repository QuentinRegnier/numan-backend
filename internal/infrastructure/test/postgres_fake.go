package test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_log"
	infradb "github.com/QuentinRegnier/numan-backend/internal/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type queryHandler func(query string, args []driver.NamedValue) (driver.Rows, error)
type execHandler func(query string, args []driver.NamedValue) (driver.Result, error)

type mockDriverConn struct {
	onQuery queryHandler
	onExec  execHandler
}

type mockTx struct{}

func (t *mockTx) Commit() error   { return nil }
func (t *mockTx) Rollback() error { return nil }

type mockStmt struct {
	conn  *mockDriverConn
	query string
}

func (s *mockStmt) Close() error  { return nil }
func (s *mockStmt) NumInput() int { return -1 }

func (s *mockStmt) Exec(args []driver.Value) (driver.Result, error) {
	named := make([]driver.NamedValue, len(args))
	for i, v := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.conn.ExecContext(context.Background(), s.query, named)
}

func (s *mockStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.conn.ExecContext(ctx, s.query, args)
}

func (s *mockStmt) Query(args []driver.Value) (driver.Rows, error) {
	named := make([]driver.NamedValue, len(args))
	for i, v := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return s.conn.QueryContext(context.Background(), s.query, named)
}

func (s *mockStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.conn.QueryContext(ctx, s.query, args)
}

func (c *mockDriverConn) Prepare(query string) (driver.Stmt, error) {
	return &mockStmt{conn: c, query: query}, nil
}

func (c *mockDriverConn) PrepareContext(_ context.Context, query string) (driver.Stmt, error) {
	return &mockStmt{conn: c, query: query}, nil
}

func (c *mockDriverConn) Close() error { return nil }

func (c *mockDriverConn) Begin() (driver.Tx, error) {
	return &mockTx{}, nil
}

func (c *mockDriverConn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	return &mockTx{}, nil
}

func (c *mockDriverConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.onQuery != nil {
		return c.onQuery(query, args)
	}
	return nil, sql.ErrNoRows
}

func (c *mockDriverConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.onExec != nil {
		return c.onExec(query, args)
	}
	return driver.RowsAffected(1), nil
}

type mockConnector struct {
	conn *mockDriverConn
}

func (c *mockConnector) Connect(context.Context) (driver.Conn, error) {
	return c.conn, nil
}

func (c *mockConnector) Driver() driver.Driver {
	return &mockDriver{}
}

type mockDriver struct{}

func (d *mockDriver) Open(name string) (driver.Conn, error) {
	return nil, errors.New("open not implemented in mock")
}

type mockRows struct {
	cols      []string
	rows      [][]driver.Value
	idx       int
	errOnNext error
}

func (r *mockRows) Columns() []string { return r.cols }
func (r *mockRows) Close() error      { return nil }
func (r *mockRows) Next(dest []driver.Value) error {
	if r.errOnNext != nil && r.idx >= len(r.rows) {
		return r.errOnNext
	}
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.idx]
	r.idx++
	for i, val := range row {
		dest[i] = val
	}
	return nil
}

func NewMockRows(cols []string, rows [][]driver.Value) *mockRows {
	return &mockRows{
		cols: cols,
		rows: rows,
	}
}

func NewMockRowsWithErr(cols []string, rows [][]driver.Value, errOnNext error) *mockRows {
	return &mockRows{
		cols:      cols,
		rows:      rows,
		errOnNext: errOnNext,
	}
}

// setupMockDB replaces infradb.PostgresDB with a mock database and returns a cleanup function.
func SetupMockDB(t *testing.T, onQuery queryHandler, onExec execHandler) (*sql.DB, func()) {
	t.Helper()
	origDB := infradb.PostgresDB

	conn := &mockDriverConn{
		onQuery: onQuery,
		onExec:  onExec,
	}
	db := sql.OpenDB(&mockConnector{conn: conn})
	infradb.PostgresDB = db

	cleanup := func() {
		if err := db.Close(); err != nil {
			numan_log.Error(context.Background()).Err(err).Msg("Impossible de fermer le curseur SQL, risque de fuite de connexion au pool")
		}
		infradb.PostgresDB = origDB
	}
	t.Cleanup(cleanup)
	return db, cleanup
}

func AssertDatabaseError_Postgres(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusInternalServerError, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeDatabaseError, appErr.Code)
}

func AssertNotFoundError_Postgres(t *testing.T, err error, expectedCode string) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
	if expectedCode != "" {
		assert.Equal(t, expectedCode, appErr.Code)
	}
}

func AssertBadRequestError_Postgres(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}
