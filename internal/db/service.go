// Package db is the Wails service the frontend talks to: one Postgres
// connection, plus the analysis and visualisation queries built on top of it.
package db

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const maxRows = 500

// Service holds a single connection. A learning tool runs one query at a
// time, and a single connection keeps search_path and transactions simple.
type Service struct {
	mu     sync.Mutex
	conn   *pgx.Conn
	dsn    string
	schema string
}

type Status struct {
	Connected bool   `json:"connected"`
	Server    string `json:"server"`
	Database  string `json:"database"`
	Schema    string `json:"schema"`
}

// Result is one statement's output. Values are Postgres's text format, so
// the frontend shows exactly what psql would; nil is SQL NULL.
type Result struct {
	Columns   []string    `json:"columns"`
	Rows      [][]*string `json:"rows"`
	RowCount  int         `json:"rowCount"`
	Truncated bool        `json:"truncated"`
	Command   string      `json:"command"`
	Millis    float64     `json:"millis"`
}

// SQLError carries the fields Postgres reports, so the frontend can explain
// constraint failures (which row, which constraint).
type SQLError struct {
	Message    string `json:"message"`
	Detail     string `json:"detail"`
	Hint       string `json:"hint"`
	Code       string `json:"code"`
	Constraint string `json:"constraint"`
	Table      string `json:"table"`
	Column     string `json:"column"`
	Position   int32  `json:"position"`
}

func toSQLError(err error) *SQLError {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return &SQLError{
			Message: pg.Message, Detail: pg.Detail, Hint: pg.Hint, Code: pg.Code,
			Constraint: pg.ConstraintName, Table: pg.TableName, Column: pg.ColumnName,
			Position: pg.Position,
		}
	}
	return &SQLError{Message: err.Error()}
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func (s *Service) Connect(dsn string) (*Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, cancel := ctx()
	defer cancel()
	if s.conn != nil {
		s.conn.Close(c)
		s.conn = nil
	}
	conn, err := pgx.Connect(c, dsn)
	if err != nil {
		return nil, err
	}
	s.conn = conn
	s.dsn = dsn
	s.schema = "public"
	return s.statusLocked(), nil
}

func (s *Service) Status() *Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

func (s *Service) statusLocked() *Status {
	if s.conn == nil {
		return &Status{}
	}
	cfg := s.conn.Config()
	return &Status{
		Connected: true,
		Server:    fmt.Sprintf("%s@%s:%d", cfg.User, cfg.Host, cfg.Port),
		Database:  cfg.Database,
		Schema:    s.schema,
	}
}

// UseSchema sets search_path, so unqualified table names resolve there.
func (s *Service) UseSchema(schema string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireConn(); err != nil {
		return err
	}
	c, cancel := ctx()
	defer cancel()
	if _, err := s.conn.Exec(c, "SELECT set_config('search_path', $1, false)", pgx.Identifier{schema}.Sanitize()+", public"); err != nil {
		return err
	}
	s.schema = schema
	return nil
}

// requireConn reconnects when the connection was closed, e.g. by a query
// that hit the timeout, and restores the schema.
func (s *Service) requireConn() error {
	if s.conn == nil {
		return errors.New("not connected")
	}
	if !s.conn.IsClosed() {
		return nil
	}
	c, cancel := ctx()
	defer cancel()
	conn, err := pgx.Connect(c, s.dsn)
	if err != nil {
		return fmt.Errorf("reconnecting: %w", err)
	}
	s.conn = conn
	_, err = s.conn.Exec(c, "SELECT set_config('search_path', $1, false)", pgx.Identifier{s.schema}.Sanitize()+", public")
	return err
}

// exec runs SQL with the simple protocol, which allows several statements
// and returns every value as text.
func (s *Service) exec(c context.Context, sql string) ([]Result, error) {
	start := time.Now()
	results, err := s.conn.PgConn().Exec(c, sql).ReadAll()
	elapsed := float64(time.Since(start).Microseconds()) / 1000
	out := make([]Result, 0, len(results))
	for _, r := range results {
		res := Result{Columns: []string{}, Rows: [][]*string{}, Command: r.CommandTag.String(), RowCount: len(r.Rows), Millis: elapsed}
		for _, f := range r.FieldDescriptions {
			res.Columns = append(res.Columns, f.Name)
		}
		for i, row := range r.Rows {
			if i == maxRows {
				res.Truncated = true
				break
			}
			vals := make([]*string, len(row))
			for j, v := range row {
				if v != nil {
					str := string(v)
					vals[j] = &str
				}
			}
			res.Rows = append(res.Rows, vals)
		}
		out = append(out, res)
	}
	return out, err
}

// execOne runs a single statement and returns its result.
func (s *Service) execOne(c context.Context, sql string) (*Result, error) {
	results, err := s.exec(c, sql)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return &Result{Columns: []string{}, Rows: [][]*string{}}, nil
	}
	return &results[len(results)-1], nil
}

type ExecuteResult struct {
	Results []Result  `json:"results"`
	Error   *SQLError `json:"error"`
}

// Execute runs the SQL for real (committed).
func (s *Service) Execute(sql string) (*ExecuteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireConn(); err != nil {
		return nil, err
	}
	c, cancel := ctx()
	defer cancel()
	results, err := s.exec(c, sql)
	return &ExecuteResult{Results: results, Error: toSQLError(err)}, nil
}

// inRollback runs fn inside a transaction that is always rolled back.
func (s *Service) inRollback(c context.Context, fn func() error) error {
	if _, err := s.conn.Exec(c, "BEGIN"); err != nil {
		return err
	}
	defer s.conn.Exec(context.Background(), "ROLLBACK")
	return fn()
}

// Plan returns EXPLAIN ANALYZE as JSON. Writes run in a rolled-back
// transaction, so nothing changes.
func (s *Service) Plan(sql string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireConn(); err != nil {
		return "", err
	}
	c, cancel := ctx()
	defer cancel()
	var plan string
	err := s.inRollback(c, func() error {
		res, err := s.execOne(c, "EXPLAIN (ANALYZE, COSTS, BUFFERS, FORMAT JSON) "+sql)
		if err != nil {
			return err
		}
		if len(res.Rows) > 0 && res.Rows[0][0] != nil {
			plan = *res.Rows[0][0]
		}
		return nil
	})
	if err != nil {
		return "", errors.New(toSQLError(err).Message)
	}
	return plan, nil
}
