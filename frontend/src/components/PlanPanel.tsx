type PlanNode = {
  "Node Type": string;
  "Relation Name"?: string;
  Alias?: string;
  "Join Type"?: string;
  "Index Name"?: string;
  "Hash Cond"?: string;
  "Merge Cond"?: string;
  "Join Filter"?: string;
  Filter?: string;
  "Index Cond"?: string;
  "Sort Key"?: string[];
  "Group Key"?: string[];
  "Actual Rows"?: number;
  "Actual Loops"?: number;
  "Actual Total Time"?: number;
  "Rows Removed by Filter"?: number;
  "Total Cost"?: number;
  Plans?: PlanNode[];
};

const hints: Record<string, string> = {
  "Seq Scan": "reads every row of the table, top to bottom",
  "Index Scan": "uses an index to jump to matching rows",
  "Index Only Scan": "answers from the index alone, never touching the table",
  "Bitmap Heap Scan": "collects matching row locations from an index, then reads them",
  "Hash Join": "builds a hash table from one side, then probes it with each row of the other",
  "Nested Loop": "for each row on one side, looks up matches on the other",
  "Merge Join": "walks two sorted inputs side by side",
  Hash: "builds the hash table for a Hash Join",
  Sort: "sorts rows",
  Aggregate: "computes count / sum / avg",
  HashAggregate: "groups rows with a hash table",
  GroupAggregate: "groups already-sorted rows",
  Limit: "stops after enough rows",
};

function Node({ n, depth }: { n: PlanNode; depth: number }) {
  const conds = [n["Hash Cond"], n["Merge Cond"], n["Join Filter"], n["Index Cond"], n.Filter].filter(Boolean);
  return (
    <div style={{ marginLeft: depth ? 20 : 0 }} className="border-l-2 border-border pl-3">
      <div className="my-1.5 rounded-md border border-border bg-panel p-2">
        <div className="flex flex-wrap items-baseline gap-2">
          <span className="font-semibold">
            {n["Join Type"] && n["Join Type"] !== "Inner" ? n["Join Type"] + " " : ""}
            {n["Node Type"]}
          </span>
          {n["Relation Name"] && (
            <span className="font-mono text-xs text-accent">
              {n["Relation Name"]}
              {n.Alias && n.Alias !== n["Relation Name"] ? " " + n.Alias : ""}
            </span>
          )}
          {n["Index Name"] && <span className="font-mono text-xs text-muted">using {n["Index Name"]}</span>}
          <span className="ml-auto text-[11px] text-muted">
            {n["Actual Rows"]} rows{n["Actual Loops"] && n["Actual Loops"] > 1 ? ` × ${n["Actual Loops"]} loops` : ""} · {n["Actual Total Time"]?.toFixed(3)} ms · cost {n["Total Cost"]}
          </span>
        </div>
        {hints[n["Node Type"]] && <div className="text-[11px] text-muted">{hints[n["Node Type"]]}</div>}
        {conds.map((c, i) => (
          <div key={i} className="font-mono text-[11px]">
            {c}
          </div>
        ))}
        {n["Rows Removed by Filter"] ? <div className="text-[11px] text-bad">{n["Rows Removed by Filter"]} rows removed by filter</div> : null}
        {n["Sort Key"] && <div className="font-mono text-[11px]">sort: {n["Sort Key"].join(", ")}</div>}
        {n["Group Key"] && <div className="font-mono text-[11px]">group: {n["Group Key"].join(", ")}</div>}
      </div>
      {n.Plans?.map((p, i) => <Node key={i} n={p} depth={depth + 1} />)}
    </div>
  );
}

export function PlanPanel({ plan }: { plan: string }) {
  let root: { Plan: PlanNode; "Planning Time": number; "Execution Time": number } | null = null;
  try {
    root = JSON.parse(plan)[0];
  } catch {
    return <pre className="p-4 text-xs">{plan}</pre>;
  }
  if (!root) return null;
  return (
    <div className="p-4">
      <p className="mb-2 text-xs text-muted">
        EXPLAIN ANALYZE: how Postgres actually ran the query. Read it inside out: the deepest nodes run first and feed their parents. Planning{" "}
        {root["Planning Time"].toFixed(3)} ms, execution {root["Execution Time"].toFixed(3)} ms.
      </p>
      <Node n={root.Plan} depth={0} />
    </div>
  );
}
