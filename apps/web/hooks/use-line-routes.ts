import { queryOptions, useQuery } from "@tanstack/react-query";

// Static projections from apps/gtfs, served independently of the live API.
export type Feed = { sha256: string; source: string; fetched_at: string; modified_at: string; start_date: string; end_date: string; attribution: string };
export type Route = { id: string; name: string; agency: string; type: number };
export type RouteShape = { id: string; path: [number, number][] };
export type RouteStop = { id: string; name: string; position: [number, number] };
export type Pattern = { id: string; route: string; direction: string; shape: string; headsign: string; stops: string[]; trips: number };
export type LineRoutes = { schema: "line-routes.v1"; feed_hash: string; line: string; routes: Route[]; patterns: Pattern[]; shapes: RouteShape[]; stops: RouteStop[] };
export type RoutesIndex = { schema: "line-routes-index.v1"; feed: Feed; lines: { line: string; file: string; patterns: number }[] };
export type RouteView = { line: string; pattern: Pattern; route: Route; shape: RouteShape; stops: RouteStop[] };

const HASH = /^[a-f0-9]{64}$/;
const FILE = /^\/gtfs\/([a-f0-9]{64})\/[a-f0-9]{64}\.json$/;

export function routeEntry(index: RoutesIndex, line: string) {
  const entry = index.lines.find((e) => e.line === line);
  if (entry && FILE.exec(entry.file)?.[1] !== index.feed.sha256) throw new Error("Invalid static route path");
  return entry;
}

export function routesIndexOptions(enabled: boolean) {
  return queryOptions({
    queryKey: ["line-routes", "index"],
    enabled,
    queryFn: async ({ signal }): Promise<RoutesIndex> => {
      const res = await fetch("/gtfs/index.json", { signal });
      if (!res.ok) throw new Error(`GTFS index answered ${res.status}`);
      const index: RoutesIndex = await res.json();
      if (index.schema !== "line-routes-index.v1" || !HASH.test(index.feed?.sha256) || !Array.isArray(index.lines)) throw new Error("Invalid GTFS index");
      for (const entry of index.lines) {
        if (typeof entry.line !== "string" || FILE.exec(entry.file)?.[1] !== index.feed.sha256) throw new Error("Invalid GTFS index entry");
      }
      return index;
    },
    staleTime: 5 * 60_000,
    retry: false,
  });
}

export function lineRoutesOptions(index: RoutesIndex | undefined, line: string | null) {
  const entry = index && line !== null ? routeEntry(index, line) : undefined;
  return queryOptions({
    queryKey: ["line-routes", index?.feed.sha256, line],
    enabled: !!entry,
    queryFn: async ({ signal }): Promise<LineRoutes> => {
      if (!index || !entry || line === null) throw new Error("No static route selected");
      const res = await fetch(entry.file, { signal });
      if (!res.ok) throw new Error(`GTFS line answered ${res.status}`);
      const data: LineRoutes = await res.json();
      if (data.schema !== "line-routes.v1" || data.feed_hash !== index.feed.sha256 || data.line !== line ||
          !Array.isArray(data.patterns) || !data.patterns.length || !Array.isArray(data.routes) || !Array.isArray(data.shapes) || !Array.isArray(data.stops)) throw new Error("Invalid GTFS line");
      // Validate every referenced sequence before any layer or stop list consumes the file.
      for (const p of data.patterns) routeView(data, p.id);
      return data;
    },
    staleTime: Infinity,
    retry: false,
  });
}

// Choose a real, deterministic pattern, preferring the one represented by most trips.
export function routeView(data: LineRoutes | undefined, id: string | null): RouteView | null {
  if (!data) return null;
  const pattern = data.patterns.find((p) => p.id === id) ?? [...data.patterns].sort((a, b) => b.trips - a.trips || a.id.localeCompare(b.id))[0];
  const route = data.routes.find((r) => r.id === pattern?.route);
  const shape = data.shapes.find((s) => s.id === pattern?.shape);
  const byId = new Map(data.stops.map((s) => [s.id, s]));
  const stops = pattern?.stops.map((id) => byId.get(id));
  const coordinate = (p: number[]) => Array.isArray(p) && p.length === 2 && p.every(Number.isFinite) && Math.abs(p[0]) <= 180 && Math.abs(p[1]) <= 90;
  if (!pattern || !route || !shape || !Array.isArray(shape.path) || shape.path.length < 2 || !shape.path.every(coordinate) ||
      !stops?.length || stops.some((s) => !s || !coordinate(s.position) || typeof s.name !== "string")) throw new Error("Invalid route geometry or stop references");
  return { line: data.line, pattern, route, shape, stops: stops as RouteStop[] };
}

export type RoutesState =
  | { kind: "none" | "loading" | "missing" }
  | { kind: "error"; retry: () => void }
  | { kind: "ready"; data: LineRoutes; feed: Feed };

export function useLineRoutes(line: string | null): RoutesState {
  const index = useQuery(routesIndexOptions(line !== null));
  const file = useQuery(lineRoutesOptions(index.data, line));
  if (line === null) return { kind: "none" };
  if (index.isError || file.isError) return { kind: "error", retry: () => { if (index.isError) void index.refetch(); else void file.refetch(); } };
  if (!index.data) return { kind: "loading" };
  if (!routeEntry(index.data, line)) return { kind: "missing" };
  if (!file.data) return { kind: "loading" };
  return { kind: "ready", data: file.data, feed: index.data.feed };
}

export function patternLabel(data: LineRoutes, p: Pattern): string {
  const r = data.routes.find((r) => r.id === p.route);
  const n = data.patterns.filter((other) => other.headsign === p.headsign && other.route === p.route);
  const variant = n.length > 1 ? ` · variante ${n.findIndex((other) => other.id === p.id) + 1}` : "";
  return `${p.headsign || r?.name || "Destino não informado"}${variant} · ${r?.agency ?? p.route}`;
}

export function feedDate(value: string): string {
  return /^\d{8}$/.test(value) ? `${value.slice(6, 8)}/${value.slice(4, 6)}/${value.slice(0, 4)}` : "não informada";
}
