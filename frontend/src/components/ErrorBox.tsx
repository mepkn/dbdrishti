import type { SQLError } from "../lib/api";
import { explainError } from "../lib/errors";

export function ErrorBox({ error }: { error: SQLError }) {
  const note = explainError(error);
  return (
    <div className="space-y-1.5 rounded-md border border-bad/40 bg-bad-soft p-3">
      <div className="font-semibold text-bad">
        {error.message}
        {error.code && <span className="ml-2 font-mono text-xs font-normal opacity-70">{error.code}</span>}
      </div>
      {error.detail && <div className="font-mono text-xs">{error.detail}</div>}
      {error.hint && <div className="text-xs">Hint: {error.hint}</div>}
      {note && <div className="border-t border-bad/30 pt-1.5 text-xs leading-relaxed">💡 {note}</div>}
    </div>
  );
}
