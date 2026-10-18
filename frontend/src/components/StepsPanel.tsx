import { useState } from "react";
import type { Step } from "../lib/api";
import { ErrorBox } from "./ErrorBox";
import { ResultGrid } from "./ResultGrid";

export function StepsPanel({ steps }: { steps: Step[] }) {
  const [sel, setSel] = useState(steps.length - 1);
  const step = steps[Math.min(sel, steps.length - 1)];
  if (!step) return null;
  return (
    <div className="flex h-full flex-col gap-3 p-4">
      <p className="text-xs text-muted">
        You write SELECT first, but Postgres runs the clauses in this order. Click a step to see the rows at that point.
      </p>
      <div className="flex flex-wrap items-center gap-1">
        {steps.map((s, i) => {
          const n = s.result?.rowCount;
          const prev = i > 0 ? steps[i - 1].result?.rowCount : undefined;
          const delta = n != null && prev != null && n !== prev ? n - prev : null;
          return (
            <div key={i} className="flex items-center gap-1">
              {i > 0 && <span className="text-muted">→</span>}
              <button
                onClick={() => setSel(i)}
                className={
                  "rounded-md border px-2.5 py-1 text-left " +
                  (i === sel ? "border-accent bg-accent text-white" : "border-border bg-panel hover:border-accent") +
                  (s.error ? " border-bad" : "")
                }
              >
                <div className="text-xs font-semibold">{s.clause}</div>
                <div className="text-[11px] opacity-80">
                  {s.error ? "error" : `${n} rows`}
                  {delta != null && <span className={i === sel ? "" : delta < 0 ? "text-bad" : "text-ok"}> ({delta > 0 ? "+" : ""}{delta})</span>}
                </div>
              </button>
            </div>
          );
        })}
      </div>
      <div className="text-sm">{step.description}</div>
      <pre className="overflow-x-auto rounded-md bg-panel-2 px-3 py-2 font-mono text-xs whitespace-pre-wrap">{step.sql}</pre>
      {step.error && <ErrorBox error={step.error} />}
      {step.result && (
        <div className="min-h-0 flex-1 overflow-auto">
          <ResultGrid columns={step.result.columns} rows={step.result.rows} />
        </div>
      )}
    </div>
  );
}
