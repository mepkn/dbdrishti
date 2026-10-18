import type { Analysis } from "../lib/api";

export function ExplainPanel({ analysis }: { analysis: Analysis | null }) {
  if (!analysis || analysis.kind === "empty") return <p className="p-3 text-muted">Type a query to see what each part does.</p>;
  if (analysis.error) {
    return (
      <div className="p-3">
        <div className="font-semibold text-bad">Syntax error</div>
        <div className="font-mono text-xs">{analysis.error.message}</div>
      </div>
    );
  }
  return (
    <div className="space-y-3 p-3">
      {analysis.warnings.map((w, i) => (
        <div key={i} className={"rounded-md border p-2 " + (w.level === "warn" ? "border-warn/40 bg-warn-soft" : "border-border bg-info-soft")}>
          <div className={"text-xs font-semibold " + (w.level === "warn" ? "text-warn" : "")}>
            {w.level === "warn" ? "⚠️ " : "ℹ️ "}
            {w.title}
          </div>
          <div className="text-xs leading-relaxed">{w.message}</div>
        </div>
      ))}
      {analysis.statements > 1 && <p className="text-xs text-muted">{analysis.statements} statements; explaining the last one.</p>}
      <ol className="space-y-1.5">
        {analysis.explanation.map((e, i) => {
          const [head, ...rest] = e.split(": ");
          return (
            <li key={i} className="flex gap-2 text-xs leading-relaxed">
              <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-accent-soft text-[10px] font-semibold text-accent">{i + 1}</span>
              <span>
                {rest.length ? (
                  <>
                    <code className="font-mono font-semibold">{head}</code>: {rest.join(": ")}
                  </>
                ) : (
                  e
                )}
              </span>
            </li>
          );
        })}
      </ol>
    </div>
  );
}
