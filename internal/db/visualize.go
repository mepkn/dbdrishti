package db

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	pg "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/proto"
)

func lastStmt(sql string) (*pg.ParseResult, *pg.Node, error) {
	tree, err := pg.Parse(sql)
	if err != nil {
		return nil, nil, errors.New(parseError(err).Message)
	}
	if len(tree.Stmts) == 0 {
		return nil, nil, errors.New("no statement")
	}
	return tree, tree.Stmts[len(tree.Stmts)-1].Stmt, nil
}

func selectNode(sel *pg.SelectStmt) *pg.Node {
	return &pg.Node{Node: &pg.Node_SelectStmt{SelectStmt: sel}}
}

func starTarget() []*pg.Node {
	return []*pg.Node{pg.MakeResTargetNodeWithVal(pg.MakeColumnRefNode([]*pg.Node{pg.MakeAStarNode()}, 0), 0)}
}

// --- Steps: the query cut at each clause, in execution order ---

type Step struct {
	Clause      string    `json:"clause"`
	Description string    `json:"description"`
	SQL         string    `json:"sql"`
	Result      *Result   `json:"result"`
	Error       *SQLError `json:"error"`
}

func (s *Service) Steps(sql string) ([]Step, error) {
	_, stmt, err := lastStmt(sql)
	if err != nil {
		return nil, err
	}
	orig := stmt.GetSelectStmt()
	if orig == nil {
		return nil, errors.New("steps work on SELECT queries")
	}
	if orig.Op != pg.SetOperation_SETOP_NONE || orig.ValuesLists != nil {
		return nil, errors.New("steps don't support UNION / INTERSECT / EXCEPT or VALUES yet")
	}

	base := func() *pg.SelectStmt {
		c := proto.Clone(orig).(*pg.SelectStmt)
		c.WhereClause, c.GroupClause, c.HavingClause, c.DistinctClause = nil, nil, nil, nil
		c.SortClause, c.LimitCount, c.LimitOffset, c.WindowClause = nil, nil, nil, nil
		c.LimitOption = pg.LimitOption_LIMIT_OPTION_DEFAULT
		c.TargetList = starTarget()
		return c
	}
	type plan struct {
		clause, desc string
		sel          *pg.SelectStmt
	}
	var plans []plan

	if len(orig.FromClause) > 0 {
		clause := "FROM"
		desc := "All rows of the table."
		for _, item := range orig.FromClause {
			if item.GetJoinExpr() != nil {
				clause, desc = "FROM + JOIN", "Rows after joining the tables, before any filtering. Each row is one combination of matching rows."
			}
		}
		if len(orig.FromClause) > 1 {
			clause, desc = "FROM", "Every combination of rows from the listed tables."
		}
		plans = append(plans, plan{clause, desc, base()})
	}
	whered := func() *pg.SelectStmt {
		c := base()
		c.WhereClause = orig.WhereClause
		return c
	}
	if orig.WhereClause != nil {
		plans = append(plans, plan{"WHERE", "Only the rows where " + exprString(orig.WhereClause) + " is true.", whered()})
	}
	if len(orig.GroupClause) > 0 {
		groups := func() *pg.SelectStmt {
			c := whered()
			c.GroupClause = orig.GroupClause
			c.TargetList = nil
			for _, g := range orig.GroupClause {
				c.TargetList = append(c.TargetList, pg.MakeResTargetNodeWithVal(g, 0))
			}
			count := pg.MakeFuncCallNode([]*pg.Node{pg.MakeStrNode("count")}, nil, 0)
			count.GetFuncCall().AggStar = true
			c.TargetList = append(c.TargetList, pg.MakeResTargetNodeWithNameAndVal("rows_in_group", count, 0))
			return c
		}
		plans = append(plans, plan{"GROUP BY", "One row per group, with how many rows were collapsed into it.", groups()})
		if orig.HavingClause != nil {
			c := groups()
			c.HavingClause = orig.HavingClause
			plans = append(plans, plan{"HAVING", "Only the groups where " + exprString(orig.HavingClause) + " is true.", c})
		}
	}
	sel := proto.Clone(orig).(*pg.SelectStmt)
	sel.DistinctClause, sel.SortClause, sel.LimitCount, sel.LimitOffset = nil, nil, nil, nil
	sel.LimitOption = pg.LimitOption_LIMIT_OPTION_DEFAULT
	plans = append(plans, plan{"SELECT", "The output columns: " + exprList(orig.TargetList) + ".", sel})
	if orig.DistinctClause != nil {
		c := proto.Clone(sel).(*pg.SelectStmt)
		c.DistinctClause = orig.DistinctClause
		plans = append(plans, plan{"DISTINCT", "Duplicate rows removed.", c})
	}
	if len(orig.SortClause) > 0 {
		c := proto.Clone(orig).(*pg.SelectStmt)
		c.LimitCount, c.LimitOffset = nil, nil
		c.LimitOption = pg.LimitOption_LIMIT_OPTION_DEFAULT
		plans = append(plans, plan{"ORDER BY", "Sorted by " + exprList(orig.SortClause) + ".", c})
	}
	if orig.LimitCount != nil || orig.LimitOffset != nil {
		plans = append(plans, plan{"LIMIT", "Only the rows LIMIT / OFFSET keep. This is the final result.", orig})
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireConn(); err != nil {
		return nil, err
	}
	c, cancel := ctx()
	defer cancel()
	steps := []Step{}
	err = s.inRollback(c, func() error {
		for _, p := range plans {
			st := Step{Clause: p.clause, Description: p.desc, SQL: deparseStmt(selectNode(p.sel))}
			if _, err := s.conn.Exec(c, "SAVEPOINT step"); err != nil {
				return err
			}
			res, err := s.execOne(c, st.SQL)
			st.Result, st.Error = res, toSQLError(err)
			if err != nil {
				s.conn.Exec(c, "ROLLBACK TO SAVEPOINT step")
			}
			steps = append(steps, st)
		}
		return nil
	})
	return steps, err
}

// --- Join view: which rows of each table pair up ---

type JoinTable struct {
	Alias   string      `json:"alias"`
	Name    string      `json:"name"`
	Columns []string    `json:"columns"`
	Rows    [][]*string `json:"rows"`
	Total   int64       `json:"total"`
	// Rows left out of Rows: paired by the join but removed by WHERE, and
	// never paired at all. Kept rows are always all shown.
	HiddenWhere   int `json:"hiddenWhere"`
	HiddenNoMatch int `json:"hiddenNoMatch"`
}

type JoinView struct {
	Tables []JoinTable `json:"tables"`
	Joins  []Join      `json:"joins"`
	Where  string      `json:"where"`
	// Each tuple holds, per table, the 0-based row index it came from, or
	// nil when that side was filled with NULLs by an outer join.
	Joined   [][]*int `json:"joined"`   // after FROM / JOIN
	Filtered [][]*int `json:"filtered"` // after WHERE too
}

// Join view shows every row the query keeps (up to keptRows) but only a few
// samples of the rows it drops, so large tables stay readable.
const (
	keptRows   = 200
	sampleRows = 5
)

func (s *Service) JoinView(sql string) (*JoinView, error) {
	_, stmt, err := lastStmt(sql)
	if err != nil {
		return nil, err
	}
	orig := stmt.GetSelectStmt()
	if orig == nil || len(orig.FromClause) == 0 {
		return nil, errors.New("join view needs a SELECT with FROM")
	}
	if orig.WithClause != nil || orig.Op != pg.SetOperation_SETOP_NONE {
		return nil, errors.New("join view doesn't support WITH or UNION yet")
	}
	sel := proto.Clone(orig).(*pg.SelectStmt)

	// Replace every table in FROM with a subquery that numbers its rows.
	var tables []*pg.RangeVar
	var replace func(n *pg.Node) error
	replace = func(n *pg.Node) error {
		if j := n.GetJoinExpr(); j != nil {
			if err := replace(j.Larg); err != nil {
				return err
			}
			return replace(j.Rarg)
		}
		rv := n.GetRangeVar()
		if rv == nil {
			return errors.New("join view only supports plain tables in FROM (no subqueries or functions)")
		}
		i := len(tables)
		tables = append(tables, rv)
		_, sub, err := lastStmt(fmt.Sprintf("SELECT row_number() OVER (ORDER BY ctid) AS __dbdrishti_%d, * FROM %s", i, quoteRel(rv)))
		if err != nil {
			return err
		}
		n.Node = &pg.Node_RangeSubselect{RangeSubselect: &pg.RangeSubselect{Subquery: sub, Alias: &pg.Alias{Aliasname: aliasOf(rv)}}}
		return nil
	}
	for _, item := range sel.FromClause {
		if err := replace(item); err != nil {
			return nil, err
		}
	}
	sel.TargetList = nil
	for i, rv := range tables {
		col := pg.MakeColumnRefNode([]*pg.Node{pg.MakeStrNode(aliasOf(rv)), pg.MakeStrNode(fmt.Sprintf("__dbdrishti_%d", i))}, 0)
		sel.TargetList = append(sel.TargetList, pg.MakeResTargetNodeWithVal(col, 0))
	}
	sel.GroupClause, sel.HavingClause, sel.DistinctClause, sel.SortClause, sel.WindowClause = nil, nil, nil, nil, nil
	sel.LimitOffset = nil
	sel.LimitCount = pg.MakeAConstIntNode(5000, 0)
	sel.LimitOption = pg.LimitOption_LIMIT_OPTION_COUNT
	filteredSQL := deparseStmt(selectNode(sel))
	sel.WhereClause = nil
	joinedSQL := deparseStmt(selectNode(sel))

	a := s.Analyze(sql)
	view := &JoinView{Joins: a.Joins, Where: exprString(orig.WhereClause)}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireConn(); err != nil {
		return nil, err
	}
	c, cancel := ctx()
	defer cancel()
	err = s.inRollback(c, func() error {
		if view.Joined, err = s.tuples(c, joinedSQL); err != nil {
			return err
		}
		if view.Filtered, err = s.tuples(c, filteredSQL); err != nil {
			return err
		}
		for ti, rv := range tables {
			t := JoinTable{Alias: aliasOf(rv), Name: relName(rv)}
			_ = s.conn.QueryRow(c, "SELECT count(*) FROM "+quoteRel(rv)).Scan(&t.Total)
			pick, hiddenWhere, hiddenNoMatch := pickRows(ti, t.Total, view.Filtered, view.Joined)
			t.HiddenWhere, t.HiddenNoMatch = hiddenWhere, hiddenNoMatch
			res, err := s.execOne(c, fmt.Sprintf(
				"SELECT * FROM (SELECT row_number() OVER (ORDER BY ctid) - 1 AS __dbdrishti_n, * FROM %s) s WHERE __dbdrishti_n = ANY ('{%s}'::bigint[]) ORDER BY __dbdrishti_n",
				quoteRel(rv), joinInts(pick)))
			if err != nil {
				return err
			}
			t.Columns = res.Columns[1:]
			pos := map[int]int{}
			for i, r := range res.Rows {
				n, _ := strconv.Atoi(*r[0])
				pos[n] = i
				t.Rows = append(t.Rows, r[1:])
			}
			if t.Rows == nil {
				t.Rows = [][]*string{}
			}
			// Point tuples at the displayed rows; rows not loaded point past
			// the end, which the frontend treats as hidden.
			for _, list := range [][][]*int{view.Joined, view.Filtered} {
				for _, tp := range list {
					if tp[ti] == nil {
						continue
					}
					i, ok := pos[*tp[ti]]
					if !ok {
						i = len(t.Rows)
					}
					tp[ti] = &i
				}
			}
			view.Tables = append(view.Tables, t)
		}
		return nil
	})
	if err != nil {
		return nil, errors.New(toSQLError(err).Message)
	}
	return view, nil
}

// pickRows chooses the row numbers of table ti to show: every row kept by
// WHERE, then a few samples of rows the join paired but WHERE removed, and of
// rows the join never paired. It also returns how many of each were left out.
func pickRows(ti int, total int64, filtered, joined [][]*int) (rows []int, hiddenWhere, hiddenNoMatch int) {
	kept, paired := map[int]bool{}, map[int]bool{}
	for _, tp := range filtered {
		if tp[ti] != nil {
			kept[*tp[ti]] = true
		}
	}
	for _, tp := range joined {
		if tp[ti] != nil {
			paired[*tp[ti]] = true
		}
	}
	where, noMatch := 0, 0
	for n := 0; n < int(total); n++ {
		switch {
		case kept[n]:
			if len(rows) < keptRows {
				rows = append(rows, n)
			}
		case paired[n]:
			if where < sampleRows {
				rows = append(rows, n)
			} else {
				hiddenWhere++
			}
			where++
		default:
			if noMatch < sampleRows {
				rows = append(rows, n)
			} else {
				hiddenNoMatch++
			}
			noMatch++
		}
	}
	sort.Ints(rows)
	return rows, hiddenWhere, hiddenNoMatch
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

func quoteRel(rv *pg.RangeVar) string {
	if rv.Schemaname != "" {
		return pgx.Identifier{rv.Schemaname, rv.Relname}.Sanitize()
	}
	return pgx.Identifier{rv.Relname}.Sanitize()
}

func (s *Service) tuples(c context.Context, sql string) ([][]*int, error) {
	res, err := s.execOne(c, sql)
	if err != nil {
		return nil, err
	}
	out := make([][]*int, 0, len(res.Rows))
	for _, row := range res.Rows {
		t := make([]*int, len(row))
		for i, v := range row {
			if v != nil {
				n, _ := strconv.Atoi(*v)
				n--
				t[i] = &n
			}
		}
		out = append(out, t)
	}
	return out, nil
}

// --- Preview: run in a transaction, show the effect, roll back ---

type RowCountChange struct {
	Table  string `json:"table"`
	Before int64  `json:"before"`
	After  int64  `json:"after"`
}

type Preview struct {
	Kind        string           `json:"kind"`
	Before      *Result          `json:"before"` // rows an UPDATE / DELETE will touch, before the change
	Results     []Result         `json:"results"`
	Error       *SQLError        `json:"error"`
	Counts      []RowCountChange `json:"counts"`      // every table whose row count would change
	SchemaAfter []Table          `json:"schemaAfter"` // for DDL
}

func (s *Service) Preview(sql string) (*Preview, error) {
	tree, stmt, err := lastStmt(sql)
	if err != nil {
		return nil, err
	}
	a := s.Analyze(sql)
	p := &Preview{Kind: a.Kind, Results: []Result{}, Counts: []RowCountChange{}, SchemaAfter: []Table{}}

	// Ask writes to return the rows they touch.
	var beforeSQL string
	star := starTarget()
	switch n := stmt.Node.(type) {
	case *pg.Node_InsertStmt:
		if len(n.InsertStmt.ReturningList) == 0 {
			n.InsertStmt.ReturningList = star
		}
	case *pg.Node_UpdateStmt:
		if len(n.UpdateStmt.ReturningList) == 0 {
			n.UpdateStmt.ReturningList = star
		}
		if len(n.UpdateStmt.FromClause) == 0 {
			beforeSQL = deparseStmt(selectNode(&pg.SelectStmt{TargetList: star, FromClause: []*pg.Node{{Node: &pg.Node_RangeVar{RangeVar: n.UpdateStmt.Relation}}}, WhereClause: n.UpdateStmt.WhereClause}))
		}
	case *pg.Node_DeleteStmt:
		if len(n.DeleteStmt.ReturningList) == 0 {
			n.DeleteStmt.ReturningList = star
		}
	}
	runSQL, err := pg.Deparse(tree)
	if err != nil {
		runSQL = sql
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireConn(); err != nil {
		return nil, err
	}
	c, cancel := ctx()
	defer cancel()
	err = s.inRollback(c, func() error {
		before, err := s.rowCounts(c)
		if err != nil {
			return err
		}
		if beforeSQL != "" {
			s.conn.Exec(c, "SAVEPOINT before")
			if p.Before, err = s.execOne(c, beforeSQL); err != nil {
				s.conn.Exec(c, "ROLLBACK TO SAVEPOINT before")
			}
		}
		results, err := s.exec(c, runSQL)
		p.Results = results
		if err != nil {
			p.Error = toSQLError(err)
			return nil
		}
		after, err := s.rowCounts(c)
		if err != nil {
			return err
		}
		for name, n := range after {
			if before[name] != n {
				p.Counts = append(p.Counts, RowCountChange{Table: name, Before: before[name], After: n})
			}
		}
		for name, n := range before {
			if _, ok := after[name]; !ok {
				p.Counts = append(p.Counts, RowCountChange{Table: name, Before: n, After: 0})
			}
		}
		if a.Kind == "ddl" {
			p.SchemaAfter, err = s.readSchema(c, s.schema)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// rowCounts counts the rows of every table in the current schema; learning
// databases are small, so exact counts are cheap.
func (s *Service) rowCounts(c context.Context) (map[string]int64, error) {
	rows, err := s.conn.Query(c, `
		SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r','p')`, s.schema)
	if err != nil {
		return nil, err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, name := range names {
		var n int64
		if err := s.conn.QueryRow(c, "SELECT count(*) FROM "+pgx.Identifier{s.schema, name}.Sanitize()).Scan(&n); err != nil {
			return nil, err
		}
		counts[name] = n
	}
	return counts, nil
}
