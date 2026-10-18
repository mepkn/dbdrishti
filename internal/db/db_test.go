package db

import (
	"encoding/json"
	"os"
	"testing"
)

func TestAnalyzeLeftJoinWhere(t *testing.T) {
	a := (&Service{}).Analyze("SELECT c.name, o.status FROM customers c LEFT JOIN orders o ON o.customer_id = c.id WHERE o.status = 'pending' AND c.email = NULL")
	if a.Kind != "select" || len(a.Joins) != 1 || a.Joins[0].Type != "LEFT" || a.Joins[0].Right != "o" {
		t.Fatalf("joins: %+v", a.Joins)
	}
	titles := map[string]bool{}
	for _, w := range a.Warnings {
		titles[w.Title] = true
	}
	if !titles["WHERE undoes the outer join"] || !titles["Comparing with NULL"] {
		t.Fatalf("warnings: %+v", a.Warnings)
	}
}

func TestAnalyzeIsNullKeepsOuterJoin(t *testing.T) {
	a := (&Service{}).Analyze("SELECT c.name FROM customers c LEFT JOIN orders o ON o.customer_id = c.id WHERE o.id IS NULL")
	for _, w := range a.Warnings {
		if w.Title == "WHERE undoes the outer join" {
			t.Fatal("IS NULL on the outer side is the anti-join pattern, not a mistake")
		}
	}
}

func TestAnalyzeParseError(t *testing.T) {
	a := (&Service{}).Analyze("SELEC * FROM x")
	if a.Error == nil || a.Error.Position == 0 {
		t.Fatalf("want parse error with position, got %+v", a.Error)
	}
}

// Integration test against a real Postgres: DRISHTI_TEST_DSN=postgres://...
func TestLive(t *testing.T) {
	dsn := os.Getenv("DRISHTI_TEST_DSN")
	if dsn == "" {
		t.Skip("DRISHTI_TEST_DSN not set")
	}
	s := &Service{}
	if _, err := s.Connect(dsn); err != nil {
		t.Fatal(err)
	}
	if err := s.LoadSample(); err != nil {
		t.Fatal(err)
	}
	if err := s.UseSchema("shop"); err != nil {
		t.Fatal(err)
	}
	tables, err := s.Schema("shop")
	if err != nil || len(tables) != 5 {
		t.Fatalf("schema: %v %d", err, len(tables))
	}
	q := "SELECT c.name, o.status FROM customers c LEFT JOIN orders o ON o.customer_id = c.id WHERE o.status <> 'cancelled' ORDER BY c.name LIMIT 3"
	v, err := s.JoinView(q)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Joined) != 7 || len(v.Filtered) != 5 {
		t.Fatalf("joined %d filtered %d", len(v.Joined), len(v.Filtered))
	}
	steps, err := s.Steps(q)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range steps {
		if st.Error != nil {
			t.Fatalf("%s: %s", st.Clause, st.Error.Message)
		}
		t.Logf("%-12s %d rows", st.Clause, st.Result.RowCount)
	}
	g, _ := s.Steps("SELECT city, count(*) FROM customers GROUP BY city HAVING count(*) > 1")
	for _, st := range g {
		if st.Error != nil {
			t.Fatalf("%s: %s", st.Clause, st.Error.Message)
		}
	}
	p, err := s.Preview("DELETE FROM customers WHERE id = 1")
	if err != nil || p.Error != nil {
		t.Fatalf("preview: %v %+v", err, p.Error)
	}
	b, _ := json.Marshal(p.Counts)
	t.Log(string(b))
	if len(p.Counts) != 3 { // customers, orders, order_items cascade
		t.Fatalf("counts %s", b)
	}
	p, _ = s.Preview("DELETE FROM products WHERE id = 1")
	if p.Error == nil || p.Error.Code != "23503" {
		t.Fatalf("want FK violation, got %+v", p.Error)
	}
	p, _ = s.Preview("ALTER TABLE customers ADD COLUMN phone text")
	if p.Error != nil || len(p.SchemaAfter) != 5 {
		t.Fatalf("ddl preview %+v", p.Error)
	}
	if _, err := s.Plan(q); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Execute("SELECT count(*) FROM customers")
	if *r.Results[0].Rows[0][0] != "5" {
		t.Fatal("preview leaked changes")
	}
}
