import { createContext, useContext, useMemo, useState, type MouseEvent } from "react";
import { Background, Controls, Handle, Position, ReactFlow, type Edge, type Node, type NodeProps } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import type { Table } from "../lib/api";
import type { Usage } from "../lib/highlight";
import { columnTip, relationTip, tableTip, type Tip } from "../lib/relations";

type TipApi = { show: (e: MouseEvent, tip: Tip | null) => void; hide: () => void; tables: Table[] };
const TipContext = createContext<TipApi | null>(null);

type TableData = { table: Table; used: boolean; cols?: Set<string>; added?: Set<string>; isNew?: boolean };

function TableNode({ data }: NodeProps<Node<TableData>>) {
  const { table, used, cols, added, isNew } = data;
  const tip = useContext(TipContext)!;
  const fkRefs = new Map(
    table.foreignKeys.flatMap((fk) =>
      fk.columns.map((c, i) => [c, { to: `${fk.refTable}.${fk.refColumns[i]}`, onDelete: fk.onDelete }] as const),
    ),
  );
  return (
    <div
      className={
        "min-w-48 rounded-lg border bg-panel shadow-sm " +
        (isNew ? "border-dashed border-ok" : used ? "border-accent ring-2 ring-accent/40" : "border-border")
      }
    >
      <div
        onMouseMove={(e) => tip.show(e, tableTip(table))}
        onMouseLeave={tip.hide}
        className={"flex justify-between rounded-t-lg px-3 py-1.5 font-semibold " + (used ? "bg-accent text-white" : "bg-panel-2")}
      >
        <span>
          {table.name}
          {tableTip(table) && <span className="ml-1.5 cursor-help font-normal opacity-75" title="">🔀 link table</span>}
        </span>
        <span className="text-[11px] font-normal opacity-75">{table.rowCount} rows</span>
      </div>
      {table.columns.map((c) => {
        const lit = cols?.has(c.name) || cols?.has("*");
        return (
          <div
            key={c.name}
            onMouseMove={(e) => tip.show(e, columnTip(tip.tables, table, c.name))}
            onMouseLeave={tip.hide}
            className={
              "relative flex gap-2 border-t border-border px-3 py-1 font-mono text-[11px] " +
              (c.pk || fkRefs.has(c.name) ? "cursor-help hover:bg-panel-2 " : "") +
              (added?.has(c.name) ? "bg-ok-soft " : lit ? "bg-accent-soft " : "")
            }
          >
            <Handle type="source" position={Position.Left} id={c.name + "-s"} className="!h-1.5 !w-1.5 !border-0 !bg-muted" />
            <span className="w-5 text-warn">{c.pk ? "🔑" : fkRefs.has(c.name) ? "🔗" : ""}</span>
            <span className={lit ? "font-semibold text-accent" : ""}>{c.name}</span>
            {fkRefs.has(c.name) && (
              <span className="text-accent" title={`ON DELETE ${fkRefs.get(c.name)!.onDelete}`}>
                → {fkRefs.get(c.name)!.to}
                {fkRefs.get(c.name)!.onDelete !== "NO ACTION" && <span className="ml-1 text-warn">({fkRefs.get(c.name)!.onDelete.toLowerCase()})</span>}
              </span>
            )}
            <span className="ml-auto pl-3 text-muted">{c.type}</span>
            <Handle type="target" position={Position.Right} id={c.name + "-t"} className="!h-1.5 !w-1.5 !border-0 !bg-muted" />
          </div>
        );
      })}
    </div>
  );
}

const nodeTypes = { table: TableNode };

// Crow's foot markers, drawn for a line arriving from the left at x = 20
// (the table edge); orient="auto-start-reverse" flips them for the start.
type End = "many" | "one" | "zeroOne";
const shapes: Record<End, string> = {
  many: "M8 10 L20 2 M8 10 L20 10 M8 10 L20 18 M0 10 L20 10",
  one: "M10 3 V17 M15 3 V17 M0 10 L20 10",
  zeroOne: "M15 3 V17 M0 10 L20 10",
};
const circles: Partial<Record<End, number>> = { many: 4, zeroOne: 7 };
const tones = { muted: "var(--muted)", accent: "var(--accent)" };

function CrowsFootDefs() {
  return (
    <svg width="0" height="0" className="absolute">
      <defs>
        {(Object.keys(shapes) as End[]).flatMap((end) =>
          (Object.keys(tones) as (keyof typeof tones)[]).map((tone) => (
            <marker
              key={end + tone}
              id={`crow-${end}-${tone}`}
              viewBox="0 0 24 20"
              refX={20}
              refY={10}
              markerWidth={24}
              markerHeight={20}
              markerUnits="userSpaceOnUse"
              orient="auto-start-reverse"
            >
              <path d={shapes[end]} fill="none" stroke={tones[tone]} strokeWidth={1.5} />
              {circles[end] != null && <circle cx={circles[end]} cy={10} r={3.5} fill="var(--panel)" stroke={tones[tone]} strokeWidth={1.5} />}
            </marker>
          )),
        )}
      </defs>
    </svg>
  );
}

// ends works out the cardinality on each side of a foreign key.
function ends(t: Table, fk: Table["foreignKeys"][number]): { child: End; parent: End } {
  const cols = t.columns.filter((c) => fk.columns.includes(c.name));
  const pkCols = t.columns.filter((c) => c.pk).map((c) => c.name);
  const unique =
    (fk.columns.length === 1 && cols[0]?.unique) ||
    (pkCols.length === fk.columns.length && fk.columns.every((c) => pkCols.includes(c)));
  return {
    child: unique ? "zeroOne" : "many", // how many child rows one parent row can have
    parent: cols.some((c) => c.nullable) ? "zeroOne" : "one", // how many parents a child row has
  };
}

// Referenced tables sit left of the tables that point at them, so arrows
// mostly flow right to left: child → parent.
function levels(tables: Table[]): Map<string, number> {
  const lv = new Map<string, number>();
  const byName = new Map(tables.map((t) => [t.name, t]));
  const visit = (name: string, seen: Set<string>): number => {
    if (lv.has(name)) return lv.get(name)!;
    if (seen.has(name)) return 0;
    seen.add(name);
    let l = 0;
    for (const fk of byName.get(name)?.foreignKeys ?? []) {
      if (fk.refTable !== name && byName.has(fk.refTable)) l = Math.max(l, visit(fk.refTable, seen) + 1);
    }
    lv.set(name, l);
    return l;
  };
  tables.forEach((t) => visit(t.name, new Set()));
  return lv;
}

type Props = { tables: Table[]; usage: Usage; before?: Table[] };

export function ErDiagram({ tables, usage, before }: Props) {
  const { nodes, edges } = useMemo(() => {
    const lv = levels(tables);
    const perLevel = new Map<number, number>();
    const prev = before && new Map(before.map((t) => [t.name, t]));
    const nodes: Node<TableData>[] = tables.map((t) => {
      const l = lv.get(t.name) ?? 0;
      const y = perLevel.get(l) ?? 0;
      perLevel.set(l, y + 80 + t.columns.length * 25);
      const old = prev?.get(t.name);
      return {
        id: t.name,
        type: "table",
        position: { x: l * 460, y },
        data: {
          table: t,
          used: usage.tables.has(t.name),
          cols: usage.columns.get(t.name),
          isNew: prev != null && !old,
          added: old ? new Set(t.columns.filter((c) => !old.columns.some((o) => o.name === c.name)).map((c) => c.name)) : undefined,
        },
      };
    });
    const edges: Edge[] = tables.flatMap((t) =>
      t.foreignKeys
        .filter((fk) => tables.some((x) => x.name === fk.refTable))
        .map((fk) => {
          const joined = usage.joinPairs.has(t.name + "|" + fk.refTable);
          const tone = joined ? "accent" : "muted";
          const card = ends(t, fk);
          return {
            id: t.name + "." + fk.name,
            data: { child: t, fk },
            interactionWidth: 16,
            source: t.name,
            sourceHandle: fk.columns[0] + "-s",
            target: fk.refTable,
            targetHandle: fk.refColumns[0] + "-t",
            type: "smoothstep",
            animated: joined,
            markerStart: `crow-${card.child}-${tone}`,
            markerEnd: `crow-${card.parent}-${tone}`,
            style: { stroke: joined ? "var(--accent)" : "var(--muted)", strokeWidth: joined ? 2 : 1.2 },
          };
        }),
    );
    return { nodes, edges };
  }, [tables, usage, before]);

  const [tip, setTip] = useState<{ x: number; y: number; tip: Tip } | null>(null);
  const api = useMemo<TipApi>(
    () => ({
      show: (e, t) => setTip(t ? { x: e.clientX, y: e.clientY, tip: t } : null),
      hide: () => setTip(null),
      tables,
    }),
    [tables],
  );

  if (tables.length === 0) return <p className="p-4 text-muted">No tables to draw.</p>;
  return (
    <TipContext.Provider value={api}>
    <div className="h-full w-full">
      <CrowsFootDefs />
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView
        minZoom={0.2}
        nodesConnectable={false}
        onEdgeMouseMove={(e, edge) => {
          const d = edge.data as { child: Table; fk: Table["foreignKeys"][number] };
          api.show(e, relationTip(tables, d.child, d.fk));
        }}
        onEdgeMouseLeave={api.hide}
        colorMode={matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"}
      >
        <Background gap={20} />
        <Controls showInteractive={false} />
      </ReactFlow>
      {tip && (
        <div
          style={{ left: Math.min(tip.x + 14, innerWidth - 400), top: Math.min(tip.y + 14, innerHeight - 40 - tip.tip.lines.length * 40) }}
          className="pointer-events-none fixed z-50 w-96 space-y-1.5 rounded-lg border border-border bg-panel p-3 text-xs leading-relaxed shadow-xl"
        >
          <div className="font-mono font-semibold">{tip.tip.title}</div>
          {tip.tip.lines.map((l, i) => (
            <p key={i} className={l.startsWith("Join them") ? "font-mono text-[11px] text-muted" : ""}>
              {l}
            </p>
          ))}
        </div>
      )}
    </div>
    </TipContext.Provider>
  );
}
