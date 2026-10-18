import type { SQLError } from "./api";

// Plain-English notes for the errors a learner meets most, keyed by SQLSTATE.
const notes: Record<string, (e: SQLError) => string> = {
  "23503": (e) =>
    e.detail.includes("still referenced")
      ? `Foreign key ${e.constraint}: another table still has rows pointing at this one, so deleting or changing it would leave them pointing at nothing. Delete those rows first, or give the key ON DELETE CASCADE / SET NULL.`
      : `Foreign key ${e.constraint}: the value you gave must already exist in the referenced table. Insert the parent row first.`,
  "23505": (e) => `Unique constraint ${e.constraint}: a row with this value already exists, and this column (or set of columns) may not repeat.`,
  "23502": (e) => `Column ${e.column} is NOT NULL: every row needs a value there, and it has no DEFAULT to fall back on.`,
  "23514": (e) => `CHECK constraint ${e.constraint}: the new value fails the rule the table defines.`,
  "42P01": () => "That table does not exist in the current schema. Check the spelling, or pick the right schema at the top.",
  "42703": () => "No such column. If you used an alias (u.name), check the alias matches the table it belongs to.",
  "42702": () => "Ambiguous column: more than one joined table has a column with this name. Prefix it with the table or alias, e.g. o.id.",
  "42803": () =>
    "With GROUP BY (or an aggregate like count), each selected column must either be in GROUP BY or be inside an aggregate. Postgres cannot pick one value out of a group for you.",
  "42601": () => "Syntax error near the marked position. Check commas, quotes and keyword order (SELECT … FROM … WHERE … GROUP BY … ORDER BY …).",
  "42883": () => "No function or operator matches these types. You may be comparing a number with text; add quotes or a cast like '5'::int.",
  "22P02": () => "The value cannot be converted to the column's type, e.g. text where a number is expected.",
  "42P10": () => "ON CONFLICT needs a column list with a unique or primary key constraint.",
};

export function explainError(e: SQLError): string | null {
  return notes[e.code]?.(e) ?? null;
}
