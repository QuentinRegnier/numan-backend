package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/QuentinRegnier/numan-backend/internal/domain/numan_error"
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

func (c *mockDriverConn) Prepare(query string) (driver.Stmt, error) {
	return nil, errors.New("prepare not implemented in mock")
}
func (c *mockDriverConn) Close() error { return nil }
func (c *mockDriverConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions not implemented in mock")
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

func newMockRows(cols []string, rows [][]driver.Value) *mockRows {
	return &mockRows{
		cols: cols,
		rows: rows,
	}
}

func newMockRowsWithErr(cols []string, rows [][]driver.Value, errOnNext error) *mockRows {
	return &mockRows{
		cols:      cols,
		rows:      rows,
		errOnNext: errOnNext,
	}
}

// setupMockDB replaces infradb.PostgresDB with a mock database and returns a cleanup function.
func setupMockDB(t *testing.T, onQuery queryHandler, onExec execHandler) (*sql.DB, func()) {
	t.Helper()
	origDB := infradb.PostgresDB

	conn := &mockDriverConn{
		onQuery: onQuery,
		onExec:  onExec,
	}
	db := sql.OpenDB(&mockConnector{conn: conn})
	infradb.PostgresDB = db

	cleanup := func() {
		_ = db.Close()
		infradb.PostgresDB = origDB
	}
	t.Cleanup(cleanup)
	return db, cleanup
}

func assertDatabaseError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusInternalServerError, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeDatabaseError, appErr.Code)
}

func assertNotFoundError(t *testing.T, err error, expectedCode string) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
	if expectedCode != "" {
		assert.Equal(t, expectedCode, appErr.Code)
	}
}

func assertBadRequestError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var appErr *numan_error.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, numan_error.CodeInvalidPayload, appErr.Code)
}
