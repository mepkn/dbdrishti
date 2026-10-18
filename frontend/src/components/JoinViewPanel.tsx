import { useMemo, useState } from "react";
import type { JoinView } from "../lib/api";
import type { Usage } from "../lib/highlight";
import { ResultGrid } from "./ResultGrid";

const HEAD = 28; // table header row height
const ROW = 26;
const GAP = 140; // width of the line area between two tables

const key = (t: (number | null)[]) => t.map((v) => (v == null ? "-" : v)).join(",");

type Status = "kept" | "where" | "nomatch";

const statusClass: Record<Status, string> = {
  kept: "",
  where: "bg-bad-soft line-through decoration-bad/60",
  nomatch: "opacity-40",
};

// used tells whether the query reads a column anywhere (SELECT, JOIN, WHERE…);
// the rest are dimmed in the tables and left out of the combined rows.
const usedBy = (usage: Usage) => (table: string, col: string) => {
  const cols = usage.columns.get(table);
  return cols != null && (cols.has("*") || cols.has(col));
};

export function JoinViewPanel({ view, usage }: { view: JoinView; usage: Usage }) {
  const used = usedBy(usage);
  const [hover, setHover] = useState<{ table: number; row: number } | null>(null);
  const filtered = useMemo(() => new Set(view.filtered.map(key)), [view]);
  const hasWhere = view.where !== "";

  const status = useMemo(
    () =>
      view.tables.map((t, ti) =>
        t.rows.map((_, ri): Status => {
          if (view.filtered.some((tp) => tp[ti] === ri)) return "kept";
          if (view.joined.some((tp) => tp[ti] === ri)) return "where";
          return "nomatch";
        }),
      ),
    [view],
  );

  const touches = (tp: (number | null)[]) => hover != null && tp[hover.table] === hover.row;

  return (
    <div className="space-y-4 p-4">
      <div className="flex flex-wrap items-center gap-x-5 gap-y-1 text-xs">
        <span>
          After {view.joins.length ? "JOIN" : "FROM"}: <b>{view.joined.length}</b> rows
        </span>
        {hasWhere && (
          <span>
            After WHERE <code className="font-mono">{view.where}</code>: <b>{view.filtered.length}</b> rows
          </span>
        )}
        <span className="ml-auto flex gap-3 text-muted">
          <span>
            <span className="mr-1 inline-block h-0.5 w-5 bg-ok align-middle" />
            pair kept
          </span>
          {hasWhere && (
            <span>
              <span className="mr-1 inline-block w-5 border-t-2 border-dashed border-bad align-middle" />
              pair removed by WHERE
            </span>
          )}
          <span className="opacity-60">faded row: no match, dropped</span>
          <span className="rounded bg-warn-soft px-1 text-warn">NULL</span>
          <span>kept with no partner</span>
        </span>
      </div>

      <div className="flex items-start overflow-x-auto pb-2">
        {view.tables.map((t, ti) => (
          <div key={ti} className="flex items-start">
            {ti > 0 && (
              <div style={{ width: GAP }} className="shrink-0">
                <div className="h-8 truncate px-2 text-center text-[11px] leading-8 font-semibold text-accent">
                  {view.joins[ti - 1]?.type ?? "×"} JOIN
                </div>
                <Lines view={view} left={ti - 1} filtered={filtered} touches={touches} hovering={hover != null} />
              </div>
            )}
            <div className="shrink-0">
              <div className="h-8 truncate text-sm leading-8 font-semibold">
                {t.name}
                {t.alias !== t.name && <span className="ml-1 font-normal text-muted">as {t.alias}</span>}
                {t.total > t.rows.length && <span className="ml-2 text-[11px] font-normal text-muted">{t.rows.length} of {t.total} rows shown</span>}
              </div>
              <table className="border-collapse rounded-md border border-border bg-panel font-mono text-xs">
                <thead>
                  <tr style={{ height: HEAD }} className="bg-panel-2">
                    {t.columns.map((c) => (
                      <th
                        key={c}
                        title={used(t.name, c) ? undefined : "not used by this query"}
                        className={"border-b border-border px-2 text-left whitespace-nowrap" + (used(t.name, c) ? "" : " font-normal opacity-30")}
                      >
                        {c}
                      </th>
                    ))}
                    <th className="border-b border-border px-1" />
                  </tr>
                </thead>
                <tbody>
                  {t.rows.map((r, ri) => {
                    const st = status[ti][ri];
                    const nullPartner = view.filtered.some((tp) => tp[ti] === ri && tp.some((v) => v == null));
                    const lit = hover?.table === ti && hover.row === ri;
                    return (
                      <tr
                        key={ri}
                        style={{ height: ROW }}
                        onMouseEnter={() => setHover({ table: ti, row: ri })}
                        onMouseLeave={() => setHover(null)}
                        className={"border-b border-border/50 " + statusClass[st] + (lit ? " outline-2 outline-accent" : "")}
                      >
                        {r.map((v, ci) => (
                          <td key={ci} className={"max-w-40 truncate px-2 whitespace-nowrap" + (used(t.name, t.columns[ci]) ? "" : " opacity-30")}>
                            {v ?? <span className="text-muted italic">NULL</span>}
                          </td>
                        ))}
                        <td className="px-1">{nullPartner && <span className="rounded bg-warn-soft px-1 text-[10px] text-warn">NULL</span>}</td>
                      </tr>
                    );
                  })}
                  {(t.hiddenWhere > 0 || t.hiddenNoMatch > 0) && (
                    <tr style={{ height: ROW }}>
                      <td colSpan={t.columns.length + 1} className="px-2 font-sans text-[11px] text-muted italic">
                        … {hiddenText(t.hiddenWhere, t.hiddenNoMatch)}
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        ))}
      </div>

      <JoinedRows view={view} filtered={filtered} used={used} />
    </div>
  );
}

type LinesProps = {
  view: JoinView;
  left: number;
  filtered: Set<string>;
  touches: (tp: (number | null)[]) => boolean;
  hovering: boolean;
};

function Lines({ view, left, filtered, touches, hovering }: LinesProps) {
  const lRows = view.tables[left].rows.length;
  const rRows = view.tables[left + 1].rows.length;
  const height = HEAD + Math.max(lRows, rRows) * ROW + 2;
  const y = (i: number) => HEAD + i * ROW + ROW / 2 + 1;
  const drawn = new Map<string, { a: number; b: number; kept: boolean; lit: boolean }>();
  for (const tp of view.joined) {
    const a = tp[left];
    const b = tp[left + 1];
    if (a == null || b == null || a >= lRows || b >= rRows) continue;
    const k = a + ":" + b;
    const prev = drawn.get(k);
    drawn.set(k, { a, b, kept: (prev?.kept ?? false) || filtered.has(key(tp)), lit: (prev?.lit ?? false) || touches(tp) });
  }
  return (
    <svg width={GAP} height={height} className="block">
      {[...drawn.values()].map(({ a, b, kept, lit }) => (
        <path
          key={a + ":" + b}
          d={`M0 ${y(a)} C ${GAP / 2} ${y(a)}, ${GAP / 2} ${y(b)}, ${GAP} ${y(b)}`}
          fill="none"
          stroke={kept ? "var(--ok)" : "var(--bad)"}
          strokeWidth={lit ? 3 : 1.5}
          strokeDasharray={kept ? undefined : "4 3"}
          opacity={hovering && !lit ? 0.12 : 0.9}
        />
      ))}
    </svg>
  );
}

function hiddenText(where: number, noMatch: number) {
  const parts: string[] = [];
  if (where) parts.push(`${where} more ${where === 1 ? "row" : "rows"} removed by WHERE`);
  if (noMatch) parts.push(`${noMatch} more with no match`);
  return parts.join(" · ") + " (not shown)";
}

// JoinedRows shows the combined rows the join produced, the table WHERE
// then filters: every kept row, and a few of the removed ones.
function JoinedRows({ view, filtered, used }: { view: JoinView; filtered: Set<string>; used: (table: string, col: string) => boolean }) {
  const keep = view.tables.map((t) => t.columns.map((c) => used(t.name, c)));
  const columns = view.tables.flatMap((t, ti) => t.columns.filter((_, ci) => keep[ti][ci]).map((c) => `${t.alias}.${c}`));
  const removed = view.joined.filter((tp) => !filtered.has(key(tp)));
  const tuples = [...view.joined.filter((tp) => filtered.has(key(tp))).slice(0, 300), ...removed.slice(0, 5)];
  const hidden = removed.length - Math.min(removed.length, 5);
  const rows = tuples.map((tp) =>
    view.tables.flatMap((t, ti) => {
      const i = tp[ti];
      const row = i == null || i >= t.rows.length ? t.columns.map(() => null) : t.rows[i];
      return row.filter((_, ci) => keep[ti][ci]);
    }),
  );
  return (
    <div className="space-y-1.5">
      <h3 className="text-xs font-semibold text-muted uppercase">
        Combined rows after the join{view.where && " (red: removed by WHERE)"}
      </h3>
      <ResultGrid
        columns={columns}
        rows={rows}
        rowClass={(i) => (filtered.has(key(tuples[i])) ? "" : "bg-bad-soft line-through decoration-bad/60")}
      />
      {hidden > 0 && <p className="text-[11px] text-muted italic">… {hidden} more rows removed by WHERE (not shown)</p>}
    </div>
  );
}
