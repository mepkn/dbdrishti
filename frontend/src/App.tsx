import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import CodeMirror from "@uiw/react-codemirror";
import { PostgreSQL, sql as sqlLang } from "@codemirror/lang-sql";
import { keymap } from "@codemirror/view";
import { Prec } from "@codemirror/state";
import { api, errorText, type Analysis, type ExecuteResult, type JoinView, type Preview, type Result, type Status, type Step, type Table } from "./lib/api";
import { usage as usageOf } from "./lib/highlight";
import { load, save } from "./lib/storage";
import { examples } from "./lib/examples";
import { SchemaTree } from "./components/SchemaTree";
import { ErDiagram } from "./components/ErDiagram";
import { JoinViewPanel } from "./components/JoinViewPanel";
import { StepsPanel } from "./components/StepsPanel";
import { PlanPanel } from "./components/PlanPanel";
import { PreviewPanel } from "./components/PreviewPanel";
import { ExplainPanel } from "./components/ExplainPanel";
import { ResultGrid } from "./components/ResultGrid";
import { ErrorBox } from "./components/ErrorBox";

type Tab = "er" | "join" | "steps" | "changes" | "plan" | "rows";
const DEFAULT_DSN = "postgres://postgres:mysecretpassword@localhost:5432/postgres";
const dark = matchMedia("(prefers-color-scheme: dark)").matches;

type HistoryItem = { sql: string; at: number; ok: boolean };

export default function App() {
  const [dsn, setDsn] = useState(() => load("dsn", DEFAULT_DSN));
  const [status, setStatus] = useState<Status | null>(null);
  const [schemas, setSchemas] = useState<string[]>([]);
  const [tables, setTables] = useState<Table[]>([]);
  const [sql, setSql] = useState(() => load("sql", examples[3].sql));
  const [analysis, setAnalysis] = useState<Analysis | null>(null);
  const [tab, setTab] = useState<Tab>("er");
  const [joinView, setJoinView] = useState<JoinView | null>(null);
  const [joinError, setJoinError] = useState("");
  const [steps, setSteps] = useState<Step[] | null>(null);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [plan, setPlan] = useState("");
  const [stepsError, setStepsError] = useState("");
  const [planError, setPlanError] = useState("");
  // Kind of the last visualized query, so empty tabs can say why they are empty.
  const [visKind, setVisKind] = useState("");
  const [result, setResult] = useState<ExecuteResult | null>(null);
  const [rows, setRows] = useState<{ table: string; result: Result } | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [hovered, setHovered] = useState<string | null>(null);
  const [history, setHistory] = useState<HistoryItem[]>(() => load("history", []));
  const [sidebar, setSidebar] = useState<"schema" | "examples" | "history">("schema");

  const schema = status?.schema ?? "public";
  const usage = useMemo(() => usageOf(analysis, tables), [analysis, tables]);

  const refreshSchema = useCallback(async (name: string) => {
    setTables((await api.Schema(name)) ?? []);
  }, []);

  const connect = useCallback(
    async (target: string) => {
      setBusy(true);
      setMessage("");
      try {
        const st = await api.Connect(target);
        save("dsn", target);
        const list = (await api.Schemas()) ?? [];
        setSchemas(list);
        const preferred = load("schema", "public");
        const pick = list.includes(preferred) ? preferred : "public";
        await api.UseSchema(pick);
        setStatus(st && { ...st, schema: pick });
        await refreshSchema(pick);
      } catch (e) {
        setStatus(null);
        setMessage("Could not connect: " + errorText(e));
      } finally {
        setBusy(false);
      }
    },
    [refreshSchema],
  );

  useEffect(() => {
    connect(dsn);
    // connect once on start with the remembered DSN
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Analysis is pure parsing in Go, cheap enough to run as you type.
  useEffect(() => {
    save("sql", sql);
    const t = setTimeout(() => api.Analyze(sql).then(setAnalysis), 200);
    return () => clearTimeout(t);
  }, [sql]);

  const switchSchema = async (name: string) => {
    await api.UseSchema(name);
    save("schema", name);
    setStatus((s) => s && { ...s, schema: name });
    await refreshSchema(name);
  };

  const loadSample = async () => {
    setBusy(true);
    try {
      await api.LoadSample();
      setSchemas((await api.Schemas()) ?? []);
      await switchSchema("shop");
      setMessage("Loaded the sample shop schema. Try the examples on the left.");
    } catch (e) {
      setMessage(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const remember = (ok: boolean) => {
    setHistory((h) => {
      const next = [{ sql, at: Date.now(), ok }, ...h.filter((x) => x.sql !== sql)].slice(0, 100);
      save("history", next);
      return next;
    });
  };

  // visualize never changes data: SELECTs run as previews too.
  const visualize = async () => {
    if (!status?.connected || !analysis || analysis.error || analysis.kind === "empty") return;
    setBusy(true);
    setMessage("");
    setPlan("");
    setPlanError("");
    setStepsError("");
    setJoinError("");
    setVisKind(analysis.kind);
    try {
      const p = await api.Preview(sql);
      setPreview(p);
      setResult(p && { results: p.results, error: p.error });
      if (analysis.kind === "select") {
        // A join needs two tables; one table has nothing to pair up.
        const joins = (analysis.tables?.length ?? 0) > 1;
        const [st, jv] = await Promise.allSettled([
          api.Steps(sql),
          joins ? api.JoinView(sql) : Promise.reject(new Error("This query reads one table, so there is no join to show. Join view pairs up rows of two or more tables, e.g. users JOIN comments ON comments.user_id = users.user_id.")),
        ]);
        setSteps(st.status === "fulfilled" ? st.value : null);
        setStepsError(st.status === "rejected" ? errorText(st.reason) : "");
        setJoinView(jv.status === "fulfilled" ? jv.value : null);
        setJoinError(jv.status === "rejected" ? errorText(jv.reason) : "");
        setTab((analysis.tables?.length ?? 0) > 1 && jv.status === "fulfilled" ? "join" : st.status === "fulfilled" ? "steps" : "er");
      } else {
        setSteps(null);
        setJoinView(null);
        setTab("changes");
      }
      remember(!p?.error);
      // The plan comes along in its own tab; a failure just leaves it hidden.
      if (p?.error) setPlanError("The query failed, so there is no plan. See the error below.");
      else if (analysis.kind === "ddl") setPlanError("DDL (CREATE, ALTER, DROP…) has no query plan: it changes the schema, it doesn't read rows.");
      else
        await api.Plan(sql).then(
          (pl) => setPlan(pl),
          (e) => setPlanError(errorText(e)),
        );
    } catch (e) {
      setMessage(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const execute = async () => {
    if (!status?.connected) return;
    setBusy(true);
    setMessage("");
    try {
      const r = await api.Execute(sql);
      setResult(r);
      remember(!r?.error);
      if (analysis?.kind !== "select") {
        setPreview(null);
        setSchemas((await api.Schemas()) ?? []);
        await refreshSchema(schema);
        if (!r?.error) setMessage((r?.results ?? []).map((x) => x.command).join(", ") + " — committed.");
      }
    } catch (e) {
      setMessage(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const openTable = async (table: string) => {
    const r = await api.TableRows(schema, table);
    if (r) {
      setRows({ table, result: r });
      setTab("rows");
    }
  };

  const actions = useRef({ visualize, execute });
  actions.current = { visualize, execute };
  const extensions = useMemo(
    () => [
      sqlLang({
        dialect: PostgreSQL,
        upperCaseKeywords: true,
        schema: Object.fromEntries(tables.map((t) => [t.name, (t.columns ?? []).map((c) => c.name)])),
      }),
      Prec.highest(
        keymap.of([
          { key: "Mod-Enter", run: () => (actions.current.visualize(), true) },
          { key: "Mod-Shift-Enter", run: () => (actions.current.execute(), true) },
        ]),
      ),
    ],
    [tables],
  );

  const tabs: { id: Tab; label: string }[] = [
    { id: "er", label: "ER diagram" },
    { id: "join", label: "Join view" },
    { id: "steps", label: "Steps" },
    { id: "changes", label: "Changes" },
    { id: "plan", label: "Plan" },
    { id: "rows", label: rows ? `Rows: ${rows.table}` : "Rows" },
  ];
  const selectOnly = (name: string, what: string) =>
    visKind && visKind !== "select"
      ? `${name} shows ${what}; it applies to SELECT queries only. See Changes for what this query would do.`
      : `Visualize a SELECT to see ${what}.`;
  const isWrite = analysis && ["insert", "update", "delete", "ddl", "other"].includes(analysis.kind);
  const last = result?.results?.[result.results.length - 1];

  return (
    <div className="flex h-full flex-col">
      <header className="drag flex h-[50px] shrink-0 items-center gap-2 border-b border-border bg-panel pr-3 pl-[92px]">
        <img src="/favicon.svg" className="h-5 w-5" alt="" />
        <span className="font-semibold">DBDrishti</span>
        <div className="no-drag ml-4 flex flex-1 items-center gap-2">
          <input
            value={dsn}
            onChange={(e) => setDsn(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && connect(dsn)}
            className="w-96 rounded-md border border-border bg-panel-2 px-2 py-1 font-mono text-xs"
            spellCheck={false}
          />
          <button onClick={() => connect(dsn)} className="rounded-md border border-border px-2.5 py-1 hover:border-accent">
            {status?.connected ? "Reconnect" : "Connect"}
          </button>
          <span className={"h-2 w-2 rounded-full " + (status?.connected ? "bg-ok" : "bg-bad")} title={status?.server} />
          {status?.connected && (
            <>
              <label className="ml-2 text-muted">schema</label>
              <select value={schema} onChange={(e) => switchSchema(e.target.value)} className="rounded-md border border-border bg-panel-2 px-1.5 py-1">
                {schemas.map((s) => (
                  <option key={s}>{s}</option>
                ))}
              </select>
              <button onClick={loadSample} className="rounded-md border border-border px-2.5 py-1 hover:border-accent" title="Drop and recreate the shop schema">
                ⟲ Load sample “shop”
              </button>
            </>
          )}
          {busy && <span className="text-muted">working…</span>}
          {message && <span className="truncate text-xs text-muted">{message}</span>}
        </div>
      </header>

      <div className="flex min-h-0 flex-1">
        <aside className="flex w-72 shrink-0 flex-col border-r border-border bg-panel">
          <div className="flex border-b border-border text-xs">
            {(["schema", "examples", "history"] as const).map((s) => (
              <button
                key={s}
                onClick={() => setSidebar(s)}
                className={"flex-1 py-2 capitalize " + (sidebar === s ? "border-b-2 border-accent font-semibold" : "text-muted")}
              >
                {s}
              </button>
            ))}
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">
            {sidebar === "schema" && <SchemaTree tables={tables} usage={usage} hovered={hovered} onHover={setHovered} onOpen={openTable} />}
            {sidebar === "examples" && (
              <div className="p-2">
                <p className="px-1 pb-2 text-[11px] text-muted">For the sample “shop” schema.</p>
                {[...new Set(examples.map((e) => e.group))].map((g) => (
                  <div key={g} className="mb-2">
                    <div className="px-1 py-1 text-[11px] font-semibold text-muted uppercase">{g}</div>
                    {examples
                      .filter((e) => e.group === g)
                      .map((e) => (
                        <button key={e.title} onClick={() => setSql(e.sql)} className="block w-full rounded px-2 py-1 text-left hover:bg-panel-2">
                          {e.title}
                        </button>
                      ))}
                  </div>
                ))}
              </div>
            )}
            {sidebar === "history" && (
              <div className="space-y-1 p-2">
                {history.length === 0 && <p className="p-1 text-muted">Queries you run show up here.</p>}
                {history.map((h) => (
                  <button key={h.at} onClick={() => setSql(h.sql)} className="block w-full rounded border border-border px-2 py-1.5 text-left hover:border-accent">
                    <div className="line-clamp-3 font-mono text-[11px] whitespace-pre-wrap">{h.sql}</div>
                    <div className={"mt-0.5 text-[10px] " + (h.ok ? "text-muted" : "text-bad")}>
                      {new Date(h.at).toLocaleString()} {h.ok ? "" : "· error"}
                    </div>
                  </button>
                ))}
              </div>
            )}
          </div>
        </aside>

        <main className="flex min-w-0 flex-1 flex-col">
          <div className="h-48 shrink-0 border-b border-border">
            <CodeMirror value={sql} onChange={setSql} extensions={extensions} theme={dark ? "dark" : "light"} height="100%" className="h-full" />
          </div>
          <div className="flex shrink-0 items-center gap-2 border-b border-border bg-panel px-3 py-1.5">
            <button
              onClick={visualize}
              disabled={!status?.connected || busy}
              className="rounded-md bg-accent px-3 py-1 font-semibold text-white disabled:opacity-50"
              title="⌘↵ — runs in a rolled-back transaction"
            >
              👁 {isWrite ? "Preview (dry run)" : "Visualize"}
            </button>
            <button
              onClick={execute}
              disabled={!status?.connected || busy}
              className={"rounded-md border px-3 py-1 disabled:opacity-50 " + (isWrite ? "border-bad text-bad" : "border-border")}
              title="⌘⇧↵ — commits changes"
            >
              ⚡ Execute
            </button>
            <span className="ml-auto text-[11px] text-muted">⌘↵ visualize · ⌘⇧↵ execute</span>
          </div>

          <div className="flex shrink-0 gap-1 border-b border-border px-2 pt-1.5">
            {tabs.map((t) => (
                <button
                  key={t.id}
                  onClick={() => setTab(t.id)}
                  className={"rounded-t-md px-3 py-1.5 text-xs " + (tab === t.id ? "bg-panel font-semibold shadow-[inset_0_-2px_0_var(--accent)]" : "text-muted hover:text-text")}
                >
                  {t.label}
                </button>
              ))}
          </div>
          <div className="relative min-h-0 flex-1 overflow-auto">
            {tab === "er" && <ErDiagram tables={tables} usage={usage} />}
            {tab === "join" &&
              (joinView ? <JoinViewPanel view={joinView} usage={usage} /> : <Empty>{joinError || selectOnly("Join view", "how rows of the joined tables pair up")}</Empty>)}
            {tab === "steps" &&
              (steps ? <StepsPanel key={sql} steps={steps} /> : <Empty>{stepsError || selectOnly("Steps", "the rows after each clause")}</Empty>)}
            {tab === "changes" &&
              (preview && visKind !== "select" ? (
                <PreviewPanel preview={preview} schema={tables} usage={usage} onCommit={execute} />
              ) : (
                <Empty>
                  {visKind === "select"
                    ? "A SELECT only reads, so nothing would change. Changes shows the dry run of INSERT, UPDATE, DELETE or DDL."
                    : "Visualize an INSERT, UPDATE, DELETE or DDL to see what it would change. It runs in a rolled-back transaction, so nothing is saved."}
                </Empty>
              ))}
            {tab === "plan" &&
              (plan ? <PlanPanel plan={plan} /> : <Empty>{planError || "Visualize a query to see how Postgres runs it (EXPLAIN ANALYZE)."}</Empty>)}
            {tab === "rows" &&
              (rows ? (
                <div className="p-4">
                  <ResultGrid columns={rows.result.columns} rows={rows.result.rows} empty="The table is empty" />
                </div>
              ) : (
                <Empty>Click a table in the schema list on the left to browse its rows.</Empty>
              ))}
          </div>

          <section className="h-56 shrink-0 overflow-auto border-t border-border bg-panel p-3">
            <div className="mb-1.5 flex items-center gap-2 text-xs font-semibold text-muted uppercase">
              Result
              {last && (
                <span className="font-normal normal-case">
                  {last.command} · {last.rowCount} rows{last.truncated ? " (showing 500)" : ""} · {last.millis} ms
                </span>
              )}
              {preview && result && analysis?.kind !== "select" && <span className="font-normal normal-case text-warn">dry run, rolled back</span>}
            </div>
            {result?.error && <ErrorBox error={result.error} />}
            {last && last.columns && <ResultGrid columns={last.columns} rows={last.rows} />}
            {!result && <p className="text-muted">Press Visualize to see the result here.</p>}
          </section>
        </main>

        <aside className="w-80 shrink-0 overflow-y-auto border-l border-border bg-panel">
          <div className="border-b border-border px-3 py-2 text-xs font-semibold text-muted uppercase">What this query does</div>
          <ExplainPanel analysis={analysis} />
        </aside>
      </div>
    </div>
  );
}

function Empty({ children }: { children: ReactNode }) {
  return <p className="p-4 text-muted">{children}</p>;
}
