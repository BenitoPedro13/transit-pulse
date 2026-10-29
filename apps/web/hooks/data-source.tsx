"use client";

import { createContext, useContext } from "react";

export type DataSource = {
  kind: "live" | "replay";
  base: string;
  mapHref: string;
  tableHref: string;
};

export const LIVE_SOURCE: DataSource = {
  kind: "live", base: "/api/v1", mapHref: "/", tableHref: "/tabela",
};
export const REPLAY_SOURCE: DataSource = {
  kind: "replay",
  base: `/replay-data/${process.env.NEXT_PUBLIC_REPLAY_CAPTURE ?? "2026-10-12"}`,
  mapHref: "/replay",
  tableHref: "/replay/tabela",
};

const DataSourceContext = createContext<DataSource>(LIVE_SOURCE);

// A stable service configuration, not a second copy of query or playback state.
export function ReplayDataProvider({ children }: { children: React.ReactNode }) {
  return <DataSourceContext value={REPLAY_SOURCE}>{children}</DataSourceContext>;
}

export function useDataSource(): DataSource {
  return useContext(DataSourceContext);
}

export function snapshotUrl(source: DataSource): string {
  return source.kind === "replay" ? `${source.base}/snapshot.json` : `${source.base}/vehicles/latest`;
}

export function historyUrl(source: DataSource, hour?: string): string {
  if (source.kind === "replay") return `${source.base}/history/${hour ?? "index"}.json`;
  return `${source.base}/history${hour ? `/${hour}` : ""}`;
}

export function selectionHref(path: string, params: { linha: string | null; t: string | null }): string {
  const query = new URLSearchParams();
  if (params.linha) query.set("linha", params.linha);
  if (params.t) query.set("t", params.t);
  return query.size ? `${path}?${query}` : path;
}
