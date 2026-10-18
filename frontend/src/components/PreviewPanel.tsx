import type { Preview, Table } from "../lib/api";
import type { Usage } from "../lib/highlight";
import { ErDiagram } from "./ErDiagram";
import { ErrorBox } from "./ErrorBox";
import { ResultGrid } from "./ResultGrid";

const verbs: Record<string, { label: string; tone: string }> = {
  insert: { label: "Rows that would be inserted", tone: "bg-ok-soft" },
  update: { label: "Rows after the update", tone: "bg-warn-soft" },
  delete: { label: "Rows that would be deleted", tone: "bg-bad-soft" },
};

type Props = { preview: Preview; schema: Table[]; usage: Usage; onCommit: () => void };

export function PreviewPanel({ preview, schema, usage, onCommit }: Props) {
  const results = preview.results ?? [];
  const last = results[results.length - 1];
  const verb = verbs[preview.kind];
  const target = preview.kind === "delete" || preview.kind === "update" || preview.kind === "insert";
  return (
    <div className="flex h-full flex-col gap-3 p-4">
      <div className="flex items-center gap-3 rounded-md border border-border bg-info-soft px-3 py-2 text-xs">
        <span>
          🧪 <b>Dry run:</b> this ran inside a transaction that was rolled back. Nothing in the database changed.
        </span>
        {!preview.error && (
          <button onClick={onCommit} className="ml-auto rounded-md bg-accent px-3 py-1 font-semibold text-white">
            Run it for real
          </button>
        )}
      </div>

      {preview.error && <ErrorBox error={preview.error} />}

      {(preview.counts ?? []).length > 0 && (
        <div className="space-y-1">
          <h3 className="text-xs font-semibold text-muted uppercase">Row counts that would change</h3>
          <div className="flex flex-wrap gap-2">
            {(preview.counts ?? []).map((c) => {
              const direct = usage.tables.has(c.table);
              const d = c.after - c.before;
              return (
                <div key={c.table} className="rounded-md border border-border bg-panel px-3 py-1.5">
                  <div className="font-semibold">{c.table}</div>
                  <div className="font-mono text-xs">
                    {c.before} → {c.after} <span className={d < 0 ? "text-bad" : "text-ok"}>({d > 0 ? "+" : ""}{d})</span>
                  </div>
                  {!direct && <div className="text-[11px] text-warn">via foreign key cascade</div>}
                </div>
              );
            })}
          </div>
        </div>
      )}

      {preview.before && preview.kind === "update" && (
        <div className="space-y-1">
          <h3 className="text-xs font-semibold text-muted uppercase">Before ({preview.before.rowCount} rows match WHERE)</h3>
          <ResultGrid columns={preview.before.columns} rows={preview.before.rows} />
        </div>
      )}

      {target && last && !preview.error && (
        <div className="space-y-1">
          <h3 className="text-xs font-semibold text-muted uppercase">
            {verb.label} ({last.rowCount})
          </h3>
          {last.rowCount === 0 ? (
            <p className="text-muted">No rows match, so nothing would change.</p>
          ) : (
            <ResultGrid columns={last.columns} rows={last.rows} rowClass={() => verb.tone} />
          )}
        </div>
      )}

      {preview.kind === "ddl" && !preview.error && (preview.schemaAfter ?? []).length > 0 && (
        <div className="relative min-h-80 flex-1 rounded-md border border-border">
          <div className="absolute top-2 left-3 z-10 rounded bg-panel/90 px-2 py-1 text-[11px] text-muted">
            Schema after this change: dashed green = new table, green row = new column
          </div>
          <ErDiagram tables={preview.schemaAfter} usage={usage} before={schema} />
        </div>
      )}

      {preview.kind === "ddl" && !preview.error && removedTables(schema, preview.schemaAfter ?? []).length > 0 && (
        <div className="text-bad">Would drop: {removedTables(schema, preview.schemaAfter ?? []).join(", ")}</div>
      )}
    </div>
  );
}

function removedTables(before: Table[], after: Table[]) {
  return before.filter((t) => !after.some((a) => a.name === t.name)).map((t) => t.name);
}
