package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	"github.com/pganalyze/pg_query_go/v6/parser"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type TableRef struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Alias  string `json:"alias"` // the name the query uses for it
}

type ColumnRef struct {
	Qualifier string `json:"qualifier"` // alias or table, empty when unqualified
	Name      string `json:"name"`      // "*" for t.* or *
}

type Join struct {
	Type      string   `json:"type"` // INNER, LEFT, RIGHT, FULL, CROSS
	Natural   bool     `json:"natural"`
	Using     []string `json:"using"`
	Condition string   `json:"condition"`
	Right     string   `json:"right"` // alias of the table being joined, when it is a plain table
}

type Warning struct {
	Level   string `json:"level"` // warn or info
	Title   string `json:"title"`
	Message string `json:"message"`
}

type ParseError struct {
	Message  string `json:"message"`
	Position int    `json:"position"` // 1-based character offset, 0 when unknown
}

type Analysis struct {
	Kind        string      `json:"kind"` // select, insert, update, delete, ddl, other
	Statement   string      `json:"statement"`
	Tables      []TableRef  `json:"tables"`
	Columns     []ColumnRef `json:"columns"`
	Joins       []Join      `json:"joins"`
	Steps       []string    `json:"steps"` // clauses in logical execution order
	Explanation []string    `json:"explanation"`
	Warnings    []Warning   `json:"warnings"`
	Error       *ParseError `json:"error"`
	Statements  int         `json:"statements"`
}

func parseError(err error) *ParseError {
	var pe *parser.Error
	if errors.As(err, &pe) {
		return &ParseError{Message: pe.Message, Position: pe.Cursorpos}
	}
	return &ParseError{Message: err.Error()}
}

// Analyze parses the SQL with Postgres's own parser and describes the last
// statement: what it touches and what each clause does. It needs no
// connection.
func (s *Service) Analyze(sql string) *Analysis {
	a := &Analysis{Tables: []TableRef{}, Columns: []ColumnRef{}, Joins: []Join{}, Steps: []string{}, Explanation: []string{}, Warnings: []Warning{}}
	if strings.TrimSpace(sql) == "" {
		a.Kind = "empty"
		return a
	}
	tree, err := pg.Parse(sql)
	if err != nil {
		a.Kind = "invalid"
		a.Error = parseError(err)
		return a
	}
	a.Statements = len(tree.Stmts)
	if len(tree.Stmts) == 0 {
		a.Kind = "empty"
		return a
	}
	stmt := tree.Stmts[len(tree.Stmts)-1].Stmt
	a.Statement = deparseStmt(stmt)
	collectRefs(stmt, a)

	switch n := stmt.Node.(type) {
	case *pg.Node_SelectStmt:
		a.Kind = "select"
		explainSelect(n.SelectStmt, a)
	case *pg.Node_InsertStmt:
		a.Kind = "insert"
		explainInsert(n.InsertStmt, a)
	case *pg.Node_UpdateStmt:
		a.Kind = "update"
		explainUpdate(n.UpdateStmt, a)
	case *pg.Node_DeleteStmt:
		a.Kind = "delete"
		explainDelete(n.DeleteStmt, a)
	case *pg.Node_CreateStmt, *pg.Node_AlterTableStmt, *pg.Node_DropStmt, *pg.Node_IndexStmt,
		*pg.Node_CreateSchemaStmt, *pg.Node_RenameStmt, *pg.Node_ViewStmt, *pg.Node_TruncateStmt:
		a.Kind = "ddl"
		a.Explanation = append(a.Explanation, "This changes the structure of the database (DDL). Preview runs it in a transaction that is rolled back and shows how the schema would change.")
	default:
		a.Kind = "other"
	}
	return a
}

// --- helpers to print parts of the tree back as SQL ---

func deparseStmt(n *pg.Node) string {
	out, err := pg.Deparse(&pg.ParseResult{Version: parseVersion, Stmts: []*pg.RawStmt{{Stmt: n}}})
	if err != nil {
		return ""
	}
	return out
}

func exprString(n *pg.Node) string {
	if n == nil {
		return ""
	}
	sel := &pg.SelectStmt{TargetList: []*pg.Node{pg.MakeResTargetNodeWithVal(n, 0)}}
	return strings.TrimPrefix(deparseStmt(&pg.Node{Node: &pg.Node_SelectStmt{SelectStmt: sel}}), "SELECT ")
}

func exprList(nodes []*pg.Node) string {
	parts := make([]string, len(nodes))
	for i, n := range nodes {
		if sb := n.GetSortBy(); sb != nil {
			parts[i] = exprString(sb.Node)
			if sb.SortbyDir == pg.SortByDir_SORTBY_DESC {
				parts[i] += " DESC"
			}
			continue
		}
		if rt := n.GetResTarget(); rt != nil {
			parts[i] = exprString(rt.Val)
			if rt.Name != "" {
				parts[i] += " AS " + rt.Name
			}
			continue
		}
		parts[i] = exprString(n)
	}
	return strings.Join(parts, ", ")
}

func fromItemString(n *pg.Node) string {
	sel := &pg.SelectStmt{
		TargetList: []*pg.Node{pg.MakeResTargetNodeWithVal(pg.MakeColumnRefNode([]*pg.Node{pg.MakeAStarNode()}, 0), 0)},
		FromClause: []*pg.Node{n},
	}
	return strings.TrimPrefix(deparseStmt(&pg.Node{Node: &pg.Node_SelectStmt{SelectStmt: sel}}), "SELECT * FROM ")
}

func toGeneric(m proto.Message) any {
	b, err := protojson.Marshal(m)
	if err != nil {
		return nil
	}
	var v any
	_ = json.Unmarshal(b, &v)
	return v
}

// walk calls fn for every object in a protojson tree; fn returns false to
// skip the object's children.
func walk(v any, fn func(key string, obj map[string]any) bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if obj, ok := child.(map[string]any); ok && !fn(k, obj) {
				continue
			}
			walk(child, fn)
		}
	case []any:
		for _, child := range t {
			walk(child, fn)
		}
	}
}

func strField(obj map[string]any, key string) string {
	s, _ := obj[key].(string)
	return s
}

func columnRefFields(obj map[string]any) []string {
	fields, _ := obj["fields"].([]any)
	out := []string{}
	for _, f := range fields {
		fm, _ := f.(map[string]any)
		if s, ok := fm["String"].(map[string]any); ok {
			out = append(out, strField(s, "sval"))
		} else if _, ok := fm["A_Star"]; ok {
			out = append(out, "*")
		}
	}
	return out
}

func collectRefs(stmt *pg.Node, a *Analysis) {
	seenT, seenC := map[string]bool{}, map[string]bool{}
	walk(toGeneric(stmt), func(key string, obj map[string]any) bool {
		switch key {
		case "RangeVar":
			t := TableRef{Schema: strField(obj, "schemaname"), Name: strField(obj, "relname")}
			t.Alias = t.Name
			if al, ok := obj["alias"].(map[string]any); ok && strField(al, "aliasname") != "" {
				t.Alias = strField(al, "aliasname")
			}
			if k := t.Schema + "." + t.Name + " " + t.Alias; !seenT[k] {
				seenT[k] = true
				a.Tables = append(a.Tables, t)
			}
		case "ColumnRef":
			f := columnRefFields(obj)
			c := ColumnRef{}
			switch {
			case len(f) == 1:
				c.Name = f[0]
			case len(f) >= 2:
				c.Qualifier, c.Name = f[len(f)-2], f[len(f)-1]
			}
			if k := c.Qualifier + "." + c.Name; c.Name != "" && !seenC[k] {
				seenC[k] = true
				a.Columns = append(a.Columns, c)
			}
		}
		return true
	})
}

func relName(rv *pg.RangeVar) string {
	if rv.Schemaname != "" {
		return rv.Schemaname + "." + rv.Relname
	}
	return rv.Relname
}

func aliasOf(rv *pg.RangeVar) string {
	if rv.Alias != nil && rv.Alias.Aliasname != "" {
		return rv.Alias.Aliasname
	}
	return rv.Relname
}

func describeRangeVar(rv *pg.RangeVar) string {
	if rv.Alias != nil && rv.Alias.Aliasname != "" {
		return fmt.Sprintf("%s (as %s)", relName(rv), rv.Alias.Aliasname)
	}
	return relName(rv)
}

func joinTypeName(j *pg.JoinExpr) string {
	switch j.Jointype {
	case pg.JoinType_JOIN_LEFT:
		return "LEFT"
	case pg.JoinType_JOIN_RIGHT:
		return "RIGHT"
	case pg.JoinType_JOIN_FULL:
		return "FULL"
	}
	if j.Quals == nil && len(j.UsingClause) == 0 && !j.IsNatural {
		return "CROSS"
	}
	return "INNER"
}

// aliasesIn lists the names a FROM item exposes.
func aliasesIn(n *pg.Node) []string {
	switch {
	case n.GetRangeVar() != nil:
		return []string{aliasOf(n.GetRangeVar())}
	case n.GetRangeSubselect() != nil && n.GetRangeSubselect().Alias != nil:
		return []string{n.GetRangeSubselect().Alias.Aliasname}
	case n.GetJoinExpr() != nil:
		return append(aliasesIn(n.GetJoinExpr().Larg), aliasesIn(n.GetJoinExpr().Rarg)...)
	}
	return nil
}

func describeFromItem(n *pg.Node) string {
	if rv := n.GetRangeVar(); rv != nil {
		return describeRangeVar(rv)
	}
	if rs := n.GetRangeSubselect(); rs != nil {
		name := "a subquery"
		if rs.Alias != nil {
			name = "the subquery " + rs.Alias.Aliasname
		}
		return name
	}
	return fromItemString(n)
}

// explainFrom walks the FROM items left to right, the order Postgres
// conceptually builds the joined rows in.
func explainFrom(items []*pg.Node, a *Analysis) {
	var visit func(n *pg.Node, first bool)
	visit = func(n *pg.Node, first bool) {
		j := n.GetJoinExpr()
		if j == nil {
			if first {
				a.Explanation = append(a.Explanation, fmt.Sprintf("FROM %s: start with every row of %s.", fromItemString(n), describeFromItem(n)))
			} else {
				a.Explanation = append(a.Explanation, fmt.Sprintf("Comma join with %s: pair every row so far with every row of %s (a cross join, unless WHERE links them).", fromItemString(n), describeFromItem(n)))
			}
			return
		}
		visit(j.Larg, first)
		join := Join{Type: joinTypeName(j), Natural: j.IsNatural, Condition: exprString(j.Quals), Using: []string{}}
		for _, u := range j.UsingClause {
			join.Using = append(join.Using, u.GetString_().GetSval())
		}
		if rv := j.Rarg.GetRangeVar(); rv != nil {
			join.Right = aliasOf(rv)
		}
		a.Joins = append(a.Joins, join)

		right := describeFromItem(j.Rarg)
		left := strings.Join(aliasesIn(j.Larg), ", ")
		var match string
		switch {
		case j.IsNatural:
			match = "rows match when every column with the same name in both sides is equal"
		case len(join.Using) > 0:
			match = fmt.Sprintf("rows match when %s is equal on both sides", strings.Join(join.Using, ", "))
		case j.Quals != nil:
			match = fmt.Sprintf("rows match when %s", join.Condition)
		}
		var text string
		switch join.Type {
		case "INNER":
			text = fmt.Sprintf("JOIN %s: %s. Only pairs that match are kept; rows with no partner on either side disappear.", right, match)
		case "LEFT":
			text = fmt.Sprintf("LEFT JOIN %s: %s. Every row from %s is kept; when nothing matches, the %s columns are NULL.", right, match, left, strings.Join(aliasesIn(j.Rarg), ", "))
		case "RIGHT":
			text = fmt.Sprintf("RIGHT JOIN %s: %s. Every row from %s is kept; when nothing matches, the %s columns are NULL.", right, match, strings.Join(aliasesIn(j.Rarg), ", "), left)
		case "FULL":
			text = fmt.Sprintf("FULL JOIN %s: %s. Rows from both sides are kept; whichever side has no match gets NULLs.", right, match)
		case "CROSS":
			text = fmt.Sprintf("CROSS JOIN %s: every row so far is paired with every row of %s (rows × rows).", right, right)
		}
		a.Explanation = append(a.Explanation, text)
	}
	for i, item := range items {
		visit(item, i == 0)
	}
}

func explainSelect(sel *pg.SelectStmt, a *Analysis) {
	if sel.Op != pg.SetOperation_SETOP_NONE {
		op := strings.TrimPrefix(sel.Op.String(), "SETOP_")
		a.Explanation = append(a.Explanation, fmt.Sprintf("%s: runs two queries and combines their rows (%s).", op, map[string]string{
			"UNION": "all rows from both, duplicates removed unless UNION ALL", "INTERSECT": "only rows present in both", "EXCEPT": "rows of the first that are not in the second",
		}[op]))
		a.Steps = append(a.Steps, op)
		return
	}
	if sel.ValuesLists != nil {
		a.Explanation = append(a.Explanation, "VALUES: builds rows from literal values, no table needed.")
		return
	}
	if sel.WithClause != nil {
		for _, cte := range sel.WithClause.Ctes {
			if c := cte.GetCommonTableExpr(); c != nil {
				a.Explanation = append(a.Explanation, fmt.Sprintf("WITH %s: first compute this subquery and treat it as a temporary table named %s.", c.Ctename, c.Ctename))
			}
		}
		a.Steps = append(a.Steps, "WITH")
	}
	if len(sel.FromClause) > 0 {
		a.Steps = append(a.Steps, "FROM")
		explainFrom(sel.FromClause, a)
		for range a.Joins {
			a.Steps = append(a.Steps, "JOIN")
		}
	}
	if sel.WhereClause != nil {
		a.Steps = append(a.Steps, "WHERE")
		a.Explanation = append(a.Explanation, fmt.Sprintf("WHERE %s: keep only the rows where this is true. NULL counts as not true, so those rows are dropped too.", exprString(sel.WhereClause)))
	}
	if len(sel.GroupClause) > 0 {
		a.Steps = append(a.Steps, "GROUP BY")
		a.Explanation = append(a.Explanation, fmt.Sprintf("GROUP BY %s: rows with the same value here collapse into one row per group. From now on you can only select these columns or aggregates (count, sum, avg...).", exprList(sel.GroupClause)))
	} else if hasAggregate(sel.TargetList) {
		a.Explanation = append(a.Explanation, "Aggregate without GROUP BY: all remaining rows form a single group, so the result is one row.")
	}
	if sel.HavingClause != nil {
		a.Steps = append(a.Steps, "HAVING")
		a.Explanation = append(a.Explanation, fmt.Sprintf("HAVING %s: like WHERE, but filters whole groups after they are formed.", exprString(sel.HavingClause)))
	}
	a.Steps = append(a.Steps, "SELECT")
	a.Explanation = append(a.Explanation, fmt.Sprintf("SELECT %s: compute the output columns for each remaining row.", exprList(sel.TargetList)))
	if sel.DistinctClause != nil {
		a.Steps = append(a.Steps, "DISTINCT")
		a.Explanation = append(a.Explanation, "DISTINCT: remove duplicate output rows.")
	}
	if len(sel.SortClause) > 0 {
		a.Steps = append(a.Steps, "ORDER BY")
		a.Explanation = append(a.Explanation, fmt.Sprintf("ORDER BY %s: sort the output (ascending unless DESC).", exprList(sel.SortClause)))
	} else if sel.LimitCount != nil {
		a.Warnings = append(a.Warnings, Warning{"info", "LIMIT without ORDER BY", "Without ORDER BY, Postgres returns rows in no guaranteed order, so which rows LIMIT keeps can change."})
	}
	if sel.LimitCount != nil || sel.LimitOffset != nil {
		a.Steps = append(a.Steps, "LIMIT")
		text := "LIMIT"
		if sel.LimitCount != nil {
			text += fmt.Sprintf(" %s: keep only the first %s rows", exprString(sel.LimitCount), exprString(sel.LimitCount))
		}
		if sel.LimitOffset != nil {
			text += fmt.Sprintf(" (after skipping %s)", exprString(sel.LimitOffset))
		}
		a.Explanation = append(a.Explanation, text+".")
	}
	warnSelect(sel, a)
}

func hasAggregate(targets []*pg.Node) bool {
	found := false
	aggs := map[string]bool{"count": true, "sum": true, "avg": true, "min": true, "max": true, "array_agg": true, "string_agg": true, "bool_and": true, "bool_or": true, "json_agg": true, "jsonb_agg": true}
	for _, t := range targets {
		walk(toGeneric(t), func(key string, obj map[string]any) bool {
			if key == "FuncCall" {
				if _, isWindow := obj["over"]; isWindow {
					return false
				}
				names, _ := obj["funcname"].([]any)
				if len(names) > 0 {
					last, _ := names[len(names)-1].(map[string]any)
					if s, ok := last["String"].(map[string]any); ok && aggs[strField(s, "sval")] {
						found = true
					}
				}
			}
			return true
		})
	}
	return found
}

// refsAliasOutsideNullTest reports whether expr reads a column of alias
// anywhere other than inside IS [NOT] NULL.
func refsAliasOutsideNullTest(expr *pg.Node, alias string) bool {
	found := false
	walk(toGeneric(expr), func(key string, obj map[string]any) bool {
		if key == "NullTest" {
			return false
		}
		if key == "ColumnRef" {
			if f := columnRefFields(obj); len(f) >= 2 && f[len(f)-2] == alias {
				found = true
			}
		}
		return true
	})
	return found
}

func warnEqualsNull(expr *pg.Node, a *Analysis) {
	if expr == nil {
		return
	}
	walk(toGeneric(expr), func(key string, obj map[string]any) bool {
		if key != "A_Expr" {
			return true
		}
		names, _ := obj["name"].([]any)
		op := ""
		if len(names) > 0 {
			if s, ok := names[0].(map[string]any)["String"].(map[string]any); ok {
				op = strField(s, "sval")
			}
		}
		if op != "=" && op != "<>" && op != "!=" {
			return true
		}
		for _, side := range []string{"lexpr", "rexpr"} {
			if sm, ok := obj[side].(map[string]any); ok {
				if c, ok := sm["A_Const"].(map[string]any); ok && c["isnull"] == true {
					fix := "IS NULL"
					if op != "=" {
						fix = "IS NOT NULL"
					}
					a.Warnings = append(a.Warnings, Warning{"warn", "Comparing with NULL", fmt.Sprintf("x %s NULL is never true, not even when x is NULL (the result is NULL). Use %s.", op, fix)})
					return false
				}
			}
		}
		return true
	})
}

func warnSelect(sel *pg.SelectStmt, a *Analysis) {
	warnEqualsNull(sel.WhereClause, a)
	warnEqualsNull(sel.HavingClause, a)

	// Outer join whose nullable side is then filtered in WHERE.
	var check func(n *pg.Node)
	check = func(n *pg.Node) {
		j := n.GetJoinExpr()
		if j == nil {
			return
		}
		check(j.Larg)
		check(j.Rarg)
		if j.Quals != nil {
			warnEqualsNull(j.Quals, a)
		}
		var nullable []string
		switch j.Jointype {
		case pg.JoinType_JOIN_LEFT:
			nullable = aliasesIn(j.Rarg)
		case pg.JoinType_JOIN_RIGHT:
			nullable = aliasesIn(j.Larg)
		}
		if sel.WhereClause != nil {
			for _, al := range nullable {
				if refsAliasOutsideNullTest(sel.WhereClause, al) {
					a.Warnings = append(a.Warnings, Warning{"warn", "WHERE undoes the outer join",
						fmt.Sprintf("Rows %s has no match for get NULL in %s's columns, and your WHERE condition on %s is not true for NULL, so those rows are dropped again. The %s JOIN now behaves like an INNER JOIN. To keep them, move the condition into the ON clause.", joinTypeName(j)+" JOIN", al, al, joinTypeName(j))})
				}
			}
		}
		if joinTypeName(j) == "CROSS" {
			a.Warnings = append(a.Warnings, Warning{"info", "Cross join", "No join condition: every row is paired with every row of the other table. Did you forget ON?"})
		}
	}
	for _, item := range sel.FromClause {
		check(item)
	}
	if len(sel.FromClause) > 1 && sel.WhereClause == nil {
		a.Warnings = append(a.Warnings, Warning{"warn", "Cartesian product", "Tables separated by commas with no WHERE are paired row × row. Use JOIN ... ON to link them."})
	}

	if len(a.Joins) > 0 || len(sel.FromClause) > 1 {
		for _, t := range sel.TargetList {
			if cr := t.GetResTarget().GetVal().GetColumnRef(); cr != nil && len(cr.Fields) == 1 && cr.Fields[0].GetAStar() != nil {
				a.Warnings = append(a.Warnings, Warning{"info", "SELECT * with a join", "You get the columns of every table, so names like id appear more than once. Pick the columns you need, e.g. u.name, o.total."})
				break
			}
		}
	}
	if len(sel.GroupClause) > 0 && sel.HavingClause == nil && sel.WhereClause != nil && hasAggregate([]*pg.Node{sel.WhereClause}) {
		a.Warnings = append(a.Warnings, Warning{"warn", "Aggregate in WHERE", "WHERE runs before grouping, so it cannot use count(), sum() and so on. Use HAVING."})
	}
}

func explainInsert(ins *pg.InsertStmt, a *Analysis) {
	target := describeRangeVar(ins.Relation)
	cols := []string{}
	for _, c := range ins.Cols {
		cols = append(cols, c.GetResTarget().GetName())
	}
	colText := "every column in table order"
	if len(cols) > 0 {
		colText = strings.Join(cols, ", ")
	}
	a.Steps = append(a.Steps, "INSERT")
	if sel := ins.SelectStmt.GetSelectStmt(); sel != nil && sel.ValuesLists != nil {
		a.Explanation = append(a.Explanation, fmt.Sprintf("INSERT INTO %s: add %d new row(s), filling %s. Columns left out get their DEFAULT, or NULL.", target, len(sel.ValuesLists), colText))
	} else {
		a.Explanation = append(a.Explanation, fmt.Sprintf("INSERT INTO %s ... SELECT: run the SELECT and add each row it returns, filling %s.", target, colText))
	}
	a.Explanation = append(a.Explanation, "Postgres checks every constraint: NOT NULL, UNIQUE, CHECK, and each foreign key must point to a row that exists.")
	if ins.OnConflictClause != nil {
		a.Explanation = append(a.Explanation, "ON CONFLICT: when a row would break a unique constraint, do the alternative action instead of failing.")
	}
}

func explainUpdate(up *pg.UpdateStmt, a *Analysis) {
	a.Steps = append(a.Steps, "UPDATE")
	a.Explanation = append(a.Explanation, fmt.Sprintf("UPDATE %s SET %s: change those columns.", describeRangeVar(up.Relation), exprListSet(up.TargetList)))
	if up.WhereClause != nil {
		a.Steps = append(a.Steps, "WHERE")
		a.Explanation = append(a.Explanation, fmt.Sprintf("WHERE %s: only in rows where this is true.", exprString(up.WhereClause)))
		warnEqualsNull(up.WhereClause, a)
	} else {
		a.Warnings = append(a.Warnings, Warning{"warn", "No WHERE", "This UPDATE changes every row in the table."})
	}
}

func exprListSet(targets []*pg.Node) string {
	parts := []string{}
	for _, t := range targets {
		if rt := t.GetResTarget(); rt != nil {
			parts = append(parts, fmt.Sprintf("%s = %s", rt.Name, exprString(rt.Val)))
		}
	}
	return strings.Join(parts, ", ")
}

func explainDelete(del *pg.DeleteStmt, a *Analysis) {
	a.Steps = append(a.Steps, "DELETE")
	a.Explanation = append(a.Explanation, fmt.Sprintf("DELETE FROM %s: remove rows.", describeRangeVar(del.Relation)))
	if del.WhereClause != nil {
		a.Steps = append(a.Steps, "WHERE")
		a.Explanation = append(a.Explanation, fmt.Sprintf("WHERE %s: only rows where this is true.", exprString(del.WhereClause)))
		warnEqualsNull(del.WhereClause, a)
	} else {
		a.Warnings = append(a.Warnings, Warning{"warn", "No WHERE", "This DELETE removes every row in the table."})
	}
	a.Explanation = append(a.Explanation, "If other tables have foreign keys pointing at these rows, Postgres refuses, or cascades / sets NULL, depending on each key's ON DELETE rule.")
}

// parseVersion is the Postgres version of the bundled parser; Deparse
// refuses trees built by hand without it.
var parseVersion = func() int32 {
	tree, _ := pg.Parse("SELECT 1")
	return tree.Version
}()
