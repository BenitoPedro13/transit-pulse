import { queryOptions, useQuery } from "@tanstack/react-query";
import { LIVE_SOURCE, snapshotUrl, useDataSource, type DataSource } from "./data-source";

// The response of GET /api/v1/vehicles/latest (apps/api, docs/tasks/TASK-live-map.md §2.3).
// null stays null: a missing speed is not a speed of 0.
export type Vehicle = {
  operator: string;
  mode: string;
  vehicle: string;
  line: string | null;
  lat: number;
  lon: number;
  event_time: string;
  speed_kmh: number | null;
  heading_deg: number | null;
};

export type Coverage = {
  operator: string;
  label: string;
  mode: string;
  tier: string;
  minute_utc: string;
  feeds_total: number;
  feeds_by_status: Record<string, number>;
};

export type Snapshot = {
  as_of_minute: string;
  generated_at: string;
  window_seconds: number;
  coverage: Coverage[];
  vehicles: Vehicle[];
};

// A vehicle plus what the screen needs from the snapshot around it.
export type VehicleRow = Vehicle & {
  operatorLabel: string;
  ageSeconds: number | null;
};

export type LatestVehicles = { snapshot: Snapshot; rows: VehicleRow[]; labels: Map<string, string> };

// A snapshot whose newest minute with positions is older than this is not live.
export const LIVE_MAX_AGE_MS = 5 * 60 * 1000;

const EM_DASH = "—";

// operator id -> the name the screen shows. An operator with no entry shows its id, never a guessed name.
export function operatorLabels(snapshot: Snapshot): Map<string, string> {
  return new Map(snapshot.coverage.map((c) => [c.operator, c.label]));
}

export function toRows(snapshot: Snapshot): VehicleRow[] {
  const labels = operatorLabels(snapshot);
  return snapshot.vehicles.map((v) => ({
    ...v,
    operatorLabel: labels.get(v.operator) ?? v.operator,
    ageSeconds: ageSeconds(v.event_time, snapshot.as_of_minute),
  }));
}

// Seconds between a fix and the end of the minute the snapshot is "as of" (the API's window ends
// there). null if the fix is after that end, which the API never sends: show it as unknown, don't
// clamp it to 0.
export function ageSeconds(eventTime: string, asOfMinute: string): number | null {
  const end = Date.parse(asOfMinute) + 60_000;
  const seconds = Math.round((end - Date.parse(eventTime)) / 1000);
  return Number.isFinite(seconds) && seconds >= 0 ? seconds : null;
}

export function formatAgeSeconds(seconds: number | null): string {
  if (seconds === null) return EM_DASH;
  if (seconds < 60) return `${seconds} s`;
  return `${Math.floor(seconds / 60)} min ${seconds % 60} s`;
}

export function formatAge(eventTime: string, asOfMinute: string): string {
  return formatAgeSeconds(ageSeconds(eventTime, asOfMinute));
}

const number = new Intl.NumberFormat("pt-BR", { maximumFractionDigits: 1 });

// null (and undefined) is "—", never 0. A reported 0 stays "0".
export function formatNullable(value: number | string | null | undefined, unit = ""): string {
  if (value === null || value === undefined) return EM_DASH;
  const text = typeof value === "number" ? number.format(value) : value;
  if (!unit) return text;
  return unit === "°" ? `${text}°` : `${text} ${unit}`;
}

const integer = new Intl.NumberFormat("pt-BR");

export function formatCount(n: number): string {
  return integer.format(n);
}

// "10.412 veículos de 3 operadores"
export function formatTotal(vehicles: number, operators: number): string {
  const v = formatCount(vehicles);
  return `${v} ${vehicles === 1 ? "veículo" : "veículos"} de ${operators} ${operators === 1 ? "operador" : "operadores"}`;
}

// Local wall-clock time; timeZone is only for tests. With seconds: "12:46:31".
export function formatClock(iso: string, timeZone?: string, seconds = false): string {
  return new Intl.DateTimeFormat("pt-BR", { hour: "2-digit", minute: "2-digit", second: seconds ? "2-digit" : undefined, timeZone }).format(new Date(iso));
}

// A prefix match, case-insensitive: typing "45" finds 457 and 458. An empty filter keeps everything.
export function filterByLine<T extends { line: string | null }>(rows: T[], query: string): T[] {
  const q = query.trim().toLowerCase();
  if (!q) return rows;
  return rows.filter((r) => r.line?.toLowerCase().startsWith(q) ?? false);
}

export type StatusKind = "live" | "stale" | "failed";
export type VehicleStatus = { kind: StatusKind; message: string };

// The one place that decides what the screen says about the data's state.
//   failed: some operator's newest minute has no feed with positions (every feed failed or has a
//           gap), so the provider's data is not arriving. It wins over the other two.
//   stale:  the newest minute with positions is more than 5 minutes old.
//   live:   otherwise.
export function statusOf(snapshot: Snapshot, now: number, timeZone?: string): VehicleStatus {
  const clock = formatClock(snapshot.as_of_minute, timeZone);
  const failed = snapshot.coverage.filter(isNotArriving);
  if (failed.length > 0) {
    return {
      kind: "failed",
      message: `Falha na coleta de dados de ${failed.map((c) => c.label).join(", ")}. Mostrando o último minuto gravado, ${clock}.`,
    };
  }
  if (now - Date.parse(snapshot.as_of_minute) > LIVE_MAX_AGE_MS) {
    return { kind: "stale", message: `Não está ao vivo: mostrando o último minuto gravado, ${clock}` };
  }
  return { kind: "live", message: `Ao vivo · atualizado ${clock}` };
}

function isNotArriving(c: Coverage): boolean {
  return (c.feeds_by_status.ok ?? 0) + (c.feeds_by_status.provider_not_ok ?? 0) === 0;
}

const STATUS_TEXT: [string, string][] = [
  ["ok", "com posições"],
  ["provider_not_ok", "sem posições válidas do fornecedor"],
  ["failed", "falha ao buscar"],
  ["gap", "lacuna: minuto não gravado"],
];

// "Ônibus municipais (SPPO): com posições em 2 de 2 fontes". The feed's own name is never shown.
export function formatCoverage(c: Coverage): string {
  const parts = STATUS_TEXT.filter(([status]) => (c.feeds_by_status[status] ?? 0) > 0).map(
    ([status, text]) => `${text} em ${c.feeds_by_status[status]} de ${c.feeds_total} ${c.feeds_total === 1 ? "fonte" : "fontes"}`,
  );
  const tier = c.tier === "live" ? "" : ` (${c.tier})`;
  return `${c.label}${tier}: ${parts.join(", ") || "sem dados"}`;
}

// The request itself, with no select: every derived hook (fleet stats, line index, ...) reads this
// one query through its own module-level select, so there is a single fetch and a single cache entry.
export function snapshotQueryOptions(source: DataSource = LIVE_SOURCE) {
  return queryOptions({
    queryKey: ["vehicles", "latest", source.base],
    queryFn: async ({ signal }): Promise<Snapshot> => {
      const url = snapshotUrl(source);
      const res = await fetch(url, { signal });
      if (!res.ok) throw new Error(`GET ${url} answered ${res.status}`);
      return res.json();
    },
    refetchInterval: source.kind === "replay" ? false : 30_000,
    staleTime: source.kind === "replay" ? Infinity : 30_000,
  });
}

const selectLatest = (snapshot: Snapshot): LatestVehicles => ({
  snapshot, rows: toRows(snapshot), labels: operatorLabels(snapshot),
});

export function latestVehiclesQueryOptions(source: DataSource = LIVE_SOURCE) {
  return queryOptions({ ...snapshotQueryOptions(source), select: selectLatest });
}

export function useLatestVehicles() {
  return useQuery(latestVehiclesQueryOptions(useDataSource()));
}
