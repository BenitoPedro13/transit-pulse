"use client";

import { formatCoverage, formatTotal } from "@/hooks/use-latest-vehicles";
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
};

// The label carries the state in words; the marker's shape and colour only add to it.
const MARKER: Record<ScreenStatus["kind"], { symbol: string; className: string }> = {
  loading: { symbol: "…", className: "text-zinc-700" },
  live: { symbol: "●", className: "text-green-800" },
  stale: { symbol: "◐", className: "text-amber-800" },
  failed: { symbol: "▲", className: "text-red-800" },
  unavailable: { symbol: "▲", className: "text-red-800" },
};

export default function StatusStrip() {
  const status = useVehicleStatus();
  const marker = MARKER[status.kind];
  const snapshot = status.snapshot;
  return (
    <div className="flex flex-col gap-1 text-sm text-zinc-800">
      <p className="font-medium">
        <span aria-hidden="true" className={marker.className}>
          {marker.symbol}{" "}
        </span>
        {status.message}
      </p>
      {snapshot && (
        <>
          <p>{formatTotal(snapshot.vehicles.length, snapshot.coverage.length)}</p>
          <ul aria-label="Cobertura por operador" className="list-disc pl-5">
            {snapshot.coverage.map((c) => (
              <li key={c.operator}>{formatCoverage(c)}</li>
            ))}
          </ul>
        </>
      )}
      <p role="status" aria-live="polite" className="sr-only">
        {ANNOUNCEMENT[status.kind]}
      </p>
    </div>
  );
}
