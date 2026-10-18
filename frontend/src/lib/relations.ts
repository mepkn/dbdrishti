import type { ForeignKey, Table } from "./api";

export type Tip = { title: string; lines: string[] };

// one turns a table name into "a user" style words; naive, but table
// names are usually plain English plurals.
export function one(table: string): string {
  const w = table.replace(/_/g, " ");
  if (w.endsWith("ies")) return w.slice(0, -3) + "y";
  if (w.endsWith("ses") || w.endsWith("xes")) return w.slice(0, -2);
  if (w.endsWith("s") && !w.endsWith("ss")) return w.slice(0, -1);
  return w;
}
const a = (word: string) => (/^[aeiou]/i.test(word) ? "an " : "a ") + word;
const cap = (s: string) => s[0].toUpperCase() + s.slice(1);

function referencing(tables: Table[], t: Table, col: string) {
  return tables.flatMap((child) =>
    child.foreignKeys.filter((fk) => fk.refTable === t.name && fk.refColumns.includes(col)).map((fk) => ({ child, fk })),
  );
}

// junction reports whether t is a link table for a many-to-many relation:
// its primary key is made of foreign keys to at least two other tables.
export function junction(t: Table): ForeignKey[] | null {
  const pk = t.columns.filter((c) => c.pk).map((c) => c.name);
  const fks = t.foreignKeys.filter((fk) => fk.refTable !== t.name);
  const fkCols = new Set(fks.flatMap((fk) => fk.columns));
  if (pk.length < 2 || !pk.every((c) => fkCols.has(c))) return null;
  const linked = fks.filter((fk) => fk.columns.every((c) => pk.includes(c)));
  return new Set(linked.map((fk) => fk.refTable)).size >= 2 ? linked : null;
}

const words = (table: string) => table.replace(/_/g, " ");

// row names one example row: "user 5 (alice)".
function row(table: string, key: string, label: string) {
  const l = label.length > 30 ? label.slice(0, 29) + "…" : label;
  return `${one(table)} ${key}${l ? ` (${l})` : ""}`;
}

function deleteRule(fk: ForeignKey, child: Table): string {
  const p = one(fk.refTable);
  const c = words(child.name);
  switch (fk.onDelete) {
    case "CASCADE":
      return `ON DELETE CASCADE: deleting ${a(p)} also deletes all of its ${c}.`;
    case "SET NULL":
      return `ON DELETE SET NULL: deleting ${a(p)} keeps its ${c}, but they no longer point at any ${p}.`;
    case "SET DEFAULT":
      return `ON DELETE SET DEFAULT: deleting ${a(p)} makes its ${c} point at the default ${p} instead.`;
    default:
      return `You can't delete ${a(p)} that still has ${c}. Delete the ${c} first.`;
  }
}

function tip(title: string, lines: (string | false | undefined)[]): Tip {
  return { title, lines: lines.filter((l): l is string => !!l) };
}

function manyToMany(t: Table, x: ForeignKey, y: ForeignKey): (string | false)[] {
  const st = x.stats;
  return [
    `${a(one(x.refTable))} can have many ${words(y.refTable)}, and ${a(one(y.refTable))} can be in many ${words(x.refTable)}. Each row of ${t.name} links one ${one(x.refTable)} to one ${one(y.refTable)}.`,
    st.sampleChild !== "" &&
      `Example: ${row(x.refTable, st.topParent, st.topLabel)} is linked to ${st.topChildren} ${st.topChildren === 1 ? one(y.refTable) : words(y.refTable)}.`,
    `The same ${one(x.refTable)} and ${one(y.refTable)} can be linked only once.`,
  ];
}

export function tableTip(t: Table): Tip | null {
  const fks = junction(t);
  if (!fks) return null;
  return tip(`🔀 ${t.name} links ${fks[0].refTable} and ${fks[1].refTable}`, manyToMany(t, fks[0], fks[1]));
}

function selfTip(t: Table, fk: ForeignKey): Tip {
  const st = fk.stats;
  const c = one(t.name);
  return tip(`🔁 ${t.name} → ${t.name}`, [
    `Each ${c} can have one parent ${c}, and a ${c} can have many child ${words(t.name)}.`,
    st.sampleChild !== "" &&
      `Example: ${row(t.name, st.sampleChild, st.childLabel)} has ${fk.columns[0]} = ${st.topParent}, so its parent is ${row(t.name, st.topParent, st.topLabel)}.`,
    `${cap(a(c))} with no parent is at the top level.`,
    fk.onDelete === "CASCADE"
      ? `ON DELETE CASCADE: deleting ${a(c)} also deletes its children, and theirs.`
      : fk.onDelete === "SET NULL"
        ? `ON DELETE SET NULL: deleting ${a(c)} moves its children to the top level.`
        : `You can't delete ${a(c)} that still has children.`,
  ]);
}

export function pkTip(tables: Table[], t: Table, col: string): Tip {
  const pk = t.columns.filter((c) => c.pk).map((c) => c.name);
  const lines: string[] = [];
  if (pk.length > 1) {
    lines.push(
      `The primary key of ${t.name} is (${pk.join(", ")}) together: the same combination can appear only once, but ${col} on its own can repeat.`,
    );
  } else {
    lines.push(
      `${col} identifies each ${one(t.name)}. All ${t.rowCount} rows have a different ${col}, and it can never be NULL, so it always points at exactly one row.`,
    );
  }
  const refs = referencing(tables, t, col);
  for (const { child, fk } of refs) {
    const cc = fk.columns[fk.refColumns.indexOf(col)];
    const link = junction(child);
    const other = link?.includes(fk) ? link.find((f) => f !== fk) : undefined;
    if (child.name === t.name) {
      lines.push(`${t.name}.${cc} stores this value to point at a parent ${one(t.name)} in the same table.`);
    } else if (other) {
      lines.push(`${child.name}.${cc} stores this value to link ${a(one(t.name))} with ${other.refTable.replace(/_/g, " ")} (many-to-many through ${child.name}).`);
    } else {
      lines.push(`${child.name}.${cc} stores this value to say which ${one(t.name)} ${a(one(child.name))} belongs to.`);
    }
  }
  if (refs.length === 0 && pk.length === 1) lines.push(`No other table points at ${t.name} yet.`);
  return { title: `🔑 ${t.name}.${col}: primary key`, lines };
}

// relationTip explains one foreign key in a few plain sentences.
export function relationTip(tables: Table[], child: Table, fk: ForeignKey): Tip {
  if (fk.refTable === child.name) return selfTip(child, fk);
  const link = junction(child);
  if (link?.includes(fk)) {
    const other = link.find((f) => f !== fk)!;
    return tip(`🔀 ${fk.refTable} ↔ ${other.refTable}, through ${child.name}`, [...manyToMany(child, fk, other), deleteRule(fk, child)]);
  }
  const st = fk.stats;
  const p = one(fk.refTable);
  const c = one(child.name);
  const col = fk.columns[0];
  const nullable = child.columns.some((x) => fk.columns.includes(x.name) && x.nullable);
  const unique =
    (fk.columns.length === 1 && child.columns.find((x) => x.name === col)?.unique) ||
    child.columns.filter((x) => x.pk).map((x) => x.name).join() === fk.columns.join();
  return tip(`🔗 ${child.name} → ${fk.refTable}`, [
    unique
      ? `Each ${p} can have at most one ${c}, and each ${c} belongs to ${nullable ? "at most" : "exactly"} one ${p}.`
      : `One ${p} can have many ${words(child.name)}; each ${c} belongs to ${nullable ? "at most" : "exactly"} one ${p}.`,
    st.sampleChild !== "" &&
      `Example: ${row(child.name, st.sampleChild, st.childLabel)} has ${col} = ${st.topParent}, so it belongs to ${row(fk.refTable, st.topParent, st.topLabel)}.`,
    `${cap(a(c))} can only use a ${col} that exists in ${fk.refTable}.`,
    nullable && `${cap(a(c))} can also have no ${p} (${col} left empty).`,
    deleteRule(fk, child),
  ]);
}

export function columnTip(tables: Table[], t: Table, col: string): Tip | null {
  const fk = t.foreignKeys.find((f) => f.columns.includes(col));
  if (fk) return relationTip(tables, t, fk);
  if (t.columns.find((c) => c.name === col)?.pk) return pkTip(tables, t, col);
  return null;
}
