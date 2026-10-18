import type { Analysis, Table } from "./api";

export type Usage = {
  tables: Set<string>;
  columns: Map<string, Set<string>>; // table name → columns the query reads
  joinPairs: Set<string>; // "a|b" table names linked by a JOIN, both orders
};

// usage maps the query's aliases back to real tables, so the schema tree
// and diagram can light up what the query touches.
export function usage(analysis: Analysis | null, schema: Table[]): Usage {
  const u: Usage = { tables: new Set(), columns: new Map(), joinPairs: new Set() };
  if (!analysis) return u;
  const byAlias = new Map<string, string>();
  for (const t of analysis.tables ?? []) {
    u.tables.add(t.name);
    byAlias.set(t.alias, t.name);
  }
  const mark = (table: string, col: string) => {
    if (!u.columns.has(table)) u.columns.set(table, new Set());
    u.columns.get(table)!.add(col);
  };
  for (const c of analysis.columns ?? []) {
    if (c.qualifier) {
      const table = byAlias.get(c.qualifier) ?? c.qualifier;
      mark(table, c.name);
    } else {
      for (const t of schema) {
        if (!u.tables.has(t.name)) continue;
        if (c.name === "*" || (t.columns ?? []).some((col) => col.name === c.name)) mark(t.name, c.name);
      }
    }
  }
  const names = (analysis.tables ?? []).map((t) => t.name);
  for (const j of analysis.joins ?? []) {
    const right = byAlias.get(j.right);
    if (!right) continue;
    for (const left of names) {
      if (left === right) continue;
      u.joinPairs.add(left + "|" + right);
      u.joinPairs.add(right + "|" + left);
    }
  }
  return u;
}
