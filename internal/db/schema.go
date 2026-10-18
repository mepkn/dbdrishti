package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type Column struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Nullable bool    `json:"nullable"`
	Default  *string `json:"default"`
	PK       bool    `json:"pk"`
	Unique   bool    `json:"unique"`
}

type ForeignKey struct {
	Name       string   `json:"name"`
	Columns    []string `json:"columns"`
	RefSchema  string   `json:"refSchema"`
	RefTable   string   `json:"refTable"`
	RefColumns []string `json:"refColumns"`
	OnDelete   string   `json:"onDelete"`
	OnUpdate   string   `json:"onUpdate"`
	Stats      FKStats  `json:"stats"`
}

// FKStats describes how the relation is used by the rows actually stored,
// so the diagram can explain it with real numbers.
type FKStats struct {
	ChildRows    int64  `json:"childRows"`    // rows in the referencing table
	NullRefs     int64  `json:"nullRefs"`     // of those, rows pointing at nothing
	ParentsUsed  int64  `json:"parentsUsed"`  // distinct parent rows referenced
	TopParent    string `json:"topParent"`    // key of the parent with the most children
	TopChildren  int64  `json:"topChildren"`  // how many children it has
	TopLabel     string `json:"topLabel"`     // a readable column of that parent, e.g. its name
	SampleParent string `json:"sampleParent"` // a parent key with children, for examples
	SampleChild  string `json:"sampleChild"`  // primary key of one child of TopParent
	ChildLabel   string `json:"childLabel"`   // a readable column of that child
}

type Table struct {
	Schema      string       `json:"schema"`
	Name        string       `json:"name"`
	Kind        string       `json:"kind"` // table or view
	Columns     []Column     `json:"columns"`
	ForeignKeys []ForeignKey `json:"foreignKeys"`
	RowCount    int64        `json:"rowCount"`
}

// Schemas lists the user schemas of the current database.
func (s *Service) Schemas() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireConn(); err != nil {
		return nil, err
	}
	c, cancel := ctx()
	defer cancel()
	rows, err := s.conn.Query(c, `
		SELECT nspname FROM pg_namespace
		WHERE nspname NOT LIKE 'pg\_%' AND nspname <> 'information_schema'
		ORDER BY nspname = 'public' DESC, nspname`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Schema describes every table and view in the schema.
func (s *Service) Schema(schema string) ([]Table, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireConn(); err != nil {
		return nil, err
	}
	c, cancel := ctx()
	defer cancel()
	return s.readSchema(c, schema)
}

func (s *Service) readSchema(c context.Context, schema string) ([]Table, error) {
	tables := []Table{}
	index := map[string]int{}

	rows, err := s.conn.Query(c, `
		SELECT c.relname, CASE WHEN c.relkind IN ('v','m') THEN 'view' ELSE 'table' END
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r','p','v','m')
		ORDER BY c.relname`, schema)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Name, &t.Kind); err != nil {
			return nil, err
		}
		t.Schema = schema
		t.Columns, t.ForeignKeys = []Column{}, []ForeignKey{}
		index[t.Name] = len(tables)
		tables = append(tables, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.conn.Query(c, `
		SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull,
		       pg_get_expr(d.adbin, d.adrelid),
		       EXISTS (SELECT 1 FROM pg_constraint k WHERE k.conrelid = c.oid AND k.contype = 'p' AND a.attnum = ANY (k.conkey)),
		       EXISTS (SELECT 1 FROM pg_constraint k WHERE k.conrelid = c.oid AND k.contype = 'u' AND k.conkey = ARRAY[a.attnum])
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = a.attnum
		WHERE n.nspname = $1 AND c.relkind IN ('r','p','v','m') AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY c.relname, a.attnum`, schema)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var table string
		var col Column
		if err := rows.Scan(&table, &col.Name, &col.Type, &col.Nullable, &col.Default, &col.PK, &col.Unique); err != nil {
			return nil, err
		}
		if i, ok := index[table]; ok {
			tables[i].Columns = append(tables[i].Columns, col)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.conn.Query(c, `
		SELECT k.conname, c.relname, rn.nspname, rc.relname,
		       ARRAY(SELECT attname FROM unnest(k.conkey) WITH ORDINALITY u(n, i) JOIN pg_attribute ON attrelid = k.conrelid AND attnum = u.n ORDER BY u.i),
		       ARRAY(SELECT attname FROM unnest(k.confkey) WITH ORDINALITY u(n, i) JOIN pg_attribute ON attrelid = k.confrelid AND attnum = u.n ORDER BY u.i),
		       k.confdeltype::text, k.confupdtype::text
		FROM pg_constraint k
		JOIN pg_class c ON c.oid = k.conrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_class rc ON rc.oid = k.confrelid
		JOIN pg_namespace rn ON rn.oid = rc.relnamespace
		WHERE n.nspname = $1 AND k.contype = 'f'
		ORDER BY c.relname, k.conname`, schema)
	if err != nil {
		return nil, err
	}
	actions := map[string]string{"a": "NO ACTION", "r": "RESTRICT", "c": "CASCADE", "n": "SET NULL", "d": "SET DEFAULT"}
	for rows.Next() {
		var table, del, upd string
		var fk ForeignKey
		if err := rows.Scan(&fk.Name, &table, &fk.RefSchema, &fk.RefTable, &fk.Columns, &fk.RefColumns, &del, &upd); err != nil {
			return nil, err
		}
		fk.OnDelete, fk.OnUpdate = actions[del], actions[upd]
		if i, ok := index[table]; ok {
			tables[i].ForeignKeys = append(tables[i].ForeignKeys, fk)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range tables {
		q := fmt.Sprintf("SELECT count(*) FROM %s", pgx.Identifier{schema, tables[i].Name}.Sanitize())
		_ = s.conn.QueryRow(c, q).Scan(&tables[i].RowCount)
	}
	for i := range tables {
		for j := range tables[i].ForeignKeys {
			s.fkStats(c, &tables[i], &tables[i].ForeignKeys[j], tables)
		}
	}
	return tables, nil
}

// TableRows returns the first rows of a table, for browsing.
func (s *Service) TableRows(schema, table string) (*Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireConn(); err != nil {
		return nil, err
	}
	c, cancel := ctx()
	defer cancel()
	return s.execOne(c, fmt.Sprintf("SELECT * FROM %s LIMIT 200", pgx.Identifier{schema, table}.Sanitize()))
}

// fkStats fills in the stats of a foreign key from its first column; errors
// leave them zero, since they only feed explanations.
func (s *Service) fkStats(c context.Context, t *Table, fk *ForeignKey, tables []Table) {
	child := pgx.Identifier{t.Schema, t.Name}.Sanitize()
	col := pgx.Identifier{fk.Columns[0]}.Sanitize()
	st := &fk.Stats
	q := fmt.Sprintf("SELECT count(*), count(*) FILTER (WHERE %s IS NULL), count(DISTINCT %s) FROM %s", col, col, child)
	if s.conn.QueryRow(c, q).Scan(&st.ChildRows, &st.NullRefs, &st.ParentsUsed) != nil {
		return
	}
	q = fmt.Sprintf("SELECT %s::text, count(*) FROM %s WHERE %s IS NOT NULL GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT 1", col, child, col)
	if s.conn.QueryRow(c, q).Scan(&st.TopParent, &st.TopChildren) != nil {
		return
	}
	st.SampleParent = st.TopParent

	for _, p := range tables {
		if p.Name == fk.RefTable && p.Schema == fk.RefSchema {
			st.TopLabel = s.label(c, &p, fk.RefColumns[0], st.TopParent)
		}
	}
	// One child of that parent, as a concrete example.
	for _, pc := range t.Columns {
		if pc.PK {
			q = fmt.Sprintf("SELECT %s::text FROM %s WHERE %s::text = $1 ORDER BY 1 LIMIT 1", pgx.Identifier{pc.Name}.Sanitize(), child, col)
			if s.conn.QueryRow(c, q, st.TopParent).Scan(&st.SampleChild) == nil {
				st.ChildLabel = s.label(c, t, pc.Name, st.SampleChild)
			}
			break
		}
	}
}

// label returns the first text column of the row whose key column equals
// key, e.g. a name, so examples read "user 5 (alice)" instead of "user 5".
func (s *Service) label(c context.Context, t *Table, keyCol, key string) string {
	for _, col := range t.Columns {
		if col.Type == "text" || strings.HasPrefix(col.Type, "character") {
			q := fmt.Sprintf("SELECT %s::text FROM %s WHERE %s::text = $1 LIMIT 1",
				pgx.Identifier{col.Name}.Sanitize(), pgx.Identifier{t.Schema, t.Name}.Sanitize(), pgx.Identifier{keyCol}.Sanitize())
			var v *string
			if s.conn.QueryRow(c, q, key).Scan(&v) == nil && v != nil {
				return *v
			}
			return ""
		}
	}
	return ""
}
