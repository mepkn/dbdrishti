# DBDrishti

Desktop app for learning SQL: type a query, see what every clause does to the rows, and see how tables link through foreign keys, before you run anything for real.

## Features

- Schema tree and ER diagram with primary keys, foreign keys (and their ON DELETE rule), unique and NOT NULL columns; tables and columns the query uses light up as you type
- Plain-English explanation of each clause in execution order, plus warnings for common mistakes (`= NULL`, WHERE undoing a LEFT JOIN, cartesian products, UPDATE/DELETE without WHERE, aggregates in WHERE)
- Join view: the joined tables side by side, with lines between the rows that pair up, rows with no match faded and pairs removed by WHERE shown in red
- Steps: the rows after FROM/JOIN → WHERE → GROUP BY → HAVING → SELECT → DISTINCT → ORDER BY → LIMIT
- Dry run for writes and DDL: runs in a transaction that is rolled back and shows the rows affected, row counts that would change (including FK cascades), foreign key errors explained, and the schema diagram after a CREATE/ALTER
- EXPLAIN ANALYZE plan as a tree
- Sample `shop` schema with one-to-many, many-to-many and self-referencing keys; example queries; query history
- Checks for updates on launch and from DBDrishti → Check for Updates… in the menu bar; installs the latest GitHub release and restarts

## Stack

Go · Wails 3 · pgx · pg_query_go (Postgres's own parser) · React 19 · TypeScript · Vite · Tailwind CSS v4 · CodeMirror · React Flow

## Development

Needs Go 1.25+, Node 22 and the Wails 3 CLI (`go install github.com/wailsapp/wails/v3/cmd/wails3@latest`), plus a Postgres to connect to:

```bash
docker run --name my-postgres -e POSTGRES_PASSWORD=mysecretpassword -p 5432:5432 -d --rm pgvector/pgvector:pg18-trixie
cd frontend && npm install && cd ..
wails3 dev
```

## Scripts

| Command | What it does |
|---|---|
| `wails3 dev` | Run with hot reload |
| `wails3 build` | Build `bin/dbdrishti` |
| `wails3 package` | Build the macOS `.app` |
| `wails3 generate bindings -ts` | Regenerate the frontend bindings after changing the Go service |
| `go test ./...` | Analyzer tests; set `DBDRISHTI_TEST_DSN=postgres://postgres:mysecretpassword@localhost:5432/postgres` to also run the live tests (they recreate the `shop` schema) |
| `npm --prefix frontend run typecheck` | Typecheck the frontend |

## How it works

- `internal/db` is the one Wails service. It holds a single connection (search_path set by the schema picker).
- `analyze.go` parses the SQL with pg_query_go, without touching the database, and builds the explanation and warnings.
- `visualize.go` rewrites the parse tree. **Steps** removes clauses one at a time. **Join view** swaps every table for `SELECT row_number() OVER (ORDER BY ctid), * FROM t`, then selects only those row numbers, with and without WHERE, to see which rows pair up. **Preview** adds `RETURNING *` and runs the statement between `BEGIN` and `ROLLBACK`.
- `sample.sql` is the `shop` schema the "Load sample" button recreates.
- **Updates** (`update.go`): release builds carry their short commit SHA (`-X main.commit`, set in `build/darwin/Taskfile.yml`). On launch and from DBDrishti → Check for Updates…, the app compares it with the tag of the latest GitHub release; if they differ it downloads `dbdrishti-macos.zip` to a temp folder, quits, swaps the `.app` in place (putting the old one back if that fails), deletes the download and relaunches. The app's folder must be writable. Builds without a SHA (`wails3 dev`) skip the check.
- `frontend/src/components` holds one panel per tab. `lib/api.ts` wraps the generated bindings.
