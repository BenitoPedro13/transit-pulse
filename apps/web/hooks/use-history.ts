import { historyUrl, LIVE_SOURCE, useDataSource, type DataSource } from "./data-source";
import { queryOptions, useQuery, useQueries, type UseQueryResult } from "@tanstack/react-query";
import { buildWindow, type HistoryIndex, type HistoryWindow, type HourFile } from "./history-window";

// The history the time machine plays (docs/tasks/TASK-time-machine.md §2.2): GET /v1/history says which
// UTC hours the API holds, and GET /v1/history/{hour} serves each as a history-hour.v1 file.

const REFRESH_MS = 30_000;

export function historyIndexOptions(source: DataSource = LIVE_SOURCE) {
  return queryOptions({
    queryKey: ["history", "index", source.base],
    queryFn: async (): Promise<HistoryIndex> => {
      const res = await fetch(historyUrl(source));
      if (!res.ok) throw new Error(`GET /api/v1/history answered ${res.status}`);
      return res.json();
    },
    refetchInterval: source.kind === 'replay' ? false : REFRESH_MS,
    staleTime: source.kind === 'replay' ? Infinity : REFRESH_MS,
  });
}

// A file with the ETag it came with. When a refetch finds the same ETag the previous object is returned
// as it is, so nothing downstream (the window, the trails' vertex buffers) is rebuilt for an hour that
// did not change.
export type HourEntry = { etag: string | null; file: HourFile };

// "2026-10-12T15:00:00Z" -> "2026-10-12T15"
export function hourPath(hourUtc: string): string {
  return hourUtc.slice(0, 13);
}

export function historyHourOptions(hourUtc: string, source: DataSource = LIVE_SOURCE) {
  const queryKey = ["history", "hour", hourUtc, source.base] as const;
  return queryOptions({
    queryKey,
    queryFn: async ({ client, signal }): Promise<HourEntry> => {
      const res = await fetch(historyUrl(source, hourPath(hourUtc)), { signal });
      if (!res.ok) throw new Error(`GET /api/v1/history/${hourPath(hourUtc)} answered ${res.status}`);
      const etag = res.headers.get("ETag");
      const previous = client.getQueryData<HourEntry>(queryKey);
      if (previous && etag !== null && previous.etag === etag) return previous;
      return { etag, file: await res.json() };
    },
    // A closed hour older than the backfill window can't change (complete: true); the current one is
    // refreshed every 30 s, the API's own cache time.
    staleTime: (query) => (source.kind === 'replay' || query.state.data?.file.complete ? Infinity : REFRESH_MS),
    refetchInterval: (query) => (source.kind === 'replay' || query.state.data?.file.complete ? false : REFRESH_MS),
    // Compared by ETag above; a deep comparison of 150 k fixes would only cost time.
    structuralSharing: false,
  });
}

export type HistoryState =
  | { kind: "loading" }
  | { kind: "error" }
  | { kind: "ready"; window: HistoryWindow };

const LOADING: HistoryState = { kind: "loading" };
const ERROR: HistoryState = { kind: "error" };
const NO_HOURS: string[] = [];

// The window is rebuilt only when a file object is a different one from last time: query results change
// whenever a fetch starts or ends, and the window is a few hundred thousand fixes.
let lastFiles: HourFile[] = [];
let lastState: HistoryState = LOADING;

// Module-level so TanStack can cache its result.
export function combineHours(results: Pick<UseQueryResult<HourEntry>, "data" | "isPending" | "isError">[]): HistoryState {
  // A partial initial window would clamp a shared minute to whichever hour arrived first
  // and rewrite the URL before its own hour loads. A settled error may leave a gap;
  // an hour that is still loading must not establish the window's bounds yet.
  if (results.some((r) => r.isPending)) return LOADING;
  const files = results.flatMap((r) => (r.data ? [r.data.file] : []));
  if (files.length === 0) return results.some((r) => r.isError) ? ERROR : LOADING;
  if (files.length === lastFiles.length && files.every((f, i) => f === lastFiles[i])) return lastState;
  lastFiles = files;
  try {
    const window = buildWindow(files);
    lastState = window ? { kind: "ready", window } : ERROR;
  } catch {
    lastState = ERROR;
  }
  return lastState;
}

// Everything the API holds, as one window. An hour that fails to load leaves its minutes as gaps; if
// none loads the state is "error", which is not the same as "loading".
export function useHistoryWindow(): HistoryState {
  const source = useDataSource();
  const index = useQuery(historyIndexOptions(source));
  const hours = index.data?.hours ?? NO_HOURS;
  const files = useQueries({ queries: hours.map((hour) => historyHourOptions(hour, source)), combine: combineHours });
  if (!index.data) return index.isError ? ERROR : LOADING;
  return files;
}
