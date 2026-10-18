import type { ReactNode } from "react";

type Props = {
  // Generated bindings type Go slices as nullable.
  columns: string[] | null;
  rows: ((string | null)[] | null)[] | null;
  rowClass?: (i: number) => string;
  rowNote?: (i: number) => ReactNode;
  empty?: string;
};

export function ResultGrid({ columns: cols, rows: data, rowClass, rowNote, empty = "No rows" }: Props) {
  const columns = cols ?? [];
  const rows = (data ?? []).map((r) => r ?? []);
  if (columns.length === 0) return null;
  return (
    <div className="overflow-auto rounded-md border border-border">
      <table className="w-full border-collapse font-mono text-xs">
        <thead className="sticky top-0 bg-panel-2">
          <tr>
            {rowNote && <th className="w-6" />}
            {columns.map((c, i) => (
              <th key={i} className="border-b border-border px-2 py-1.5 text-left font-semibold whitespace-nowrap">
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className={"border-b border-border/60 last:border-0 " + (rowClass?.(i) ?? "")}>
              {rowNote && <td className="px-1 text-center">{rowNote(i)}</td>}
              {r.map((v, j) => (
                <td key={j} className="max-w-72 truncate px-2 py-1 whitespace-nowrap" title={v ?? "NULL"}>
                  {v == null ? <span className="text-muted italic">NULL</span> : v}
                </td>
              ))}
            </tr>
          ))}
          {rows.length === 0 && (
            <tr>
              <td colSpan={columns.length + (rowNote ? 1 : 0)} className="px-2 py-3 text-center text-muted">
                {empty}
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
