"use client";

import { useVehicleStatus, type ScreenStatus } from "@/hooks/use-vehicle-status";

// Said once when the status changes (live → stale → failed), not on every refresh. The visible
// text below updates freely; only this region is a live region, and its text depends on the kind
// of status alone.
const ANNOUNCEMENT: Record<ScreenStatus["kind"], string> = {
  loading: "",
  live: "Ao vivo.",
  stale: "Não está ao vivo.",
  failed: "Atenção: falha na coleta de dados de um ou mais operadores.",
  unavailable: "Não foi possível carregar os veículos.",
  replay: "Reprodução do histórico, não é ao vivo.",
};

// The label carries the state in words; the marker's shape and colour only add to it.
const MARKER: Record<ScreenStatus["kind"], { symbol: string; className: string }> = {
  loading: { symbol: "…", className: "text-neblina" },
  live: { symbol: "●", className: "text-status-live" },
  stale: { symbol: "◐", className: "text-status-stale" },
  failed: { symbol: "▲", className: "text-status-failed" },
  unavailable: { symbol: "▲", className: "text-status-failed" },
  replay: { symbol: "↺", className: "text-status-stale" },
};

export default function StatusStrip() {
  const status = useVehicleStatus();
  const marker = MARKER[status.kind];
  return (
    <div className="pointer-events-auto rounded-md border border-borda bg-morro px-3 py-1.5 text-xs font-medium text-calcadao lg:text-sm">
      <p>
        <span aria-hidden="true" className={marker.className}>
          {marker.symbol}{" "}
        </span>
        {status.message}
      </p>
      <p role="status" aria-live="polite" className="sr-only">
        {ANNOUNCEMENT[status.kind]}
      </p>
    </div>
  );
}
