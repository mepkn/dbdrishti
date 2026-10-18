import { useState } from "react";
import type { Table } from "../lib/api";
import type { Usage } from "../lib/highlight";

type Props = {
  tables: Table[];
  usage: Usage;
  hovered: string | null;
  onHover: (key: string | null) => void;
  onOpen: (table: string) => void;
};

function Badge({ children, className }: { children: string; className: string }) {
  return <span className={"rounded px-1 text-[10px] font-semibold " + className}>{children}</span>;
}

export function SchemaTree({ tables, usage, hovered, onHover, onOpen }: Props) {
  const [closed, setClosed] = useState<Set<string>>(new Set());
  if (tables.length === 0) return <p className="p-3 text-muted">No tables in this schema yet.</p>;
  return (
    <div className="space-y-1 p-2">
      {tables.map((t) => {
        const used = usage.tables.has(t.name);
        const cols = usage.columns.get(t.name);
        const fkCols = new Map(t.foreignKeys.flatMap((fk) => fk.columns.map((c, i) => [c, `${fk.refTable}.${fk.refColumns[i]}`] as const)));
        const open = !closed.has(t.name);
        return (
          <div key={t.name} className={"rounded-md border " + (used ? "border-accent bg-accent-soft/50" : "border-transparent")}>
            <div className="flex items-center gap-1 px-1.5 py-1">
              <button
                className="w-4 text-muted"
                onClick={() => setClosed((s) => (s.delete(t.name) ? new Set(s) : new Set(s).add(t.name)))}
              >
                {open ? "▾" : "▸"}
              </button>
              <button className="flex-1 truncate text-left font-semibold hover:text-accent" onClick={() => onOpen(t.name)} title="Show rows">
                {t.name}
              </button>
              <span className="text-[11px] text-muted">{t.kind === "view" ? "view" : `${t.rowCount} rows`}</span>
            </div>
            {open && (
              <ul className="pb-1">
                {t.columns.map((c) => {
                  const key = `${t.name}.${c.name}`;
                  const ref = fkCols.get(c.name);
                  const lit = cols?.has(c.name) || cols?.has("*");
                  const linked = hovered != null && (hovered === key || hovered === ref || fkTarget(tables, hovered) === key);
                  return (
                    <li
                      key={c.name}
                      onMouseEnter={() => onHover(ref ? key : null)}
                      onMouseLeave={() => onHover(null)}
                      className={
                        "flex items-center gap-1.5 py-0.5 pr-1.5 pl-6 font-mono text-xs " +
                        (linked ? "bg-warn-soft " : lit ? "text-accent " : "")
                      }
                      title={`${c.type}${c.nullable ? "" : " NOT NULL"}${c.default ? " DEFAULT " + c.default : ""}`}
                    >
                      <span className={"truncate " + (lit ? "font-semibold" : "")}>{c.name}</span>
                      <span className="truncate text-[11px] text-muted">{c.type}</span>
                      <span className="ml-auto flex shrink-0 gap-0.5">
                        {c.pk && <Badge className="bg-warn-soft text-warn">PK</Badge>}
                        {ref && <Badge className="bg-info-soft text-accent">{"FK→" + ref}</Badge>}
                        {c.unique && <Badge className="bg-panel-2 text-muted">UQ</Badge>}
                        {!c.nullable && !c.pk && <Badge className="bg-panel-2 text-muted">NN</Badge>}
                      </span>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        );
      })}
    </div>
  );
}

// fkTarget returns the column a hovered FK column points at.
function fkTarget(tables: Table[], key: string): string | null {
  const [table, col] = key.split(".");
  for (const t of tables) {
    if (t.name !== table) continue;
    for (const fk of t.foreignKeys) {
      const i = fk.columns.indexOf(col);
      if (i >= 0) return `${fk.refTable}.${fk.refColumns[i]}`;
    }
  }
  return null;
}
