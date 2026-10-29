import { useDataSource } from "./data-source";
import { useQuery } from "@tanstack/react-query";
import { snapshotQueryOptions, type Snapshot, type Vehicle } from "./use-latest-vehicles";
import { compareLines } from "./use-lines";

// At or above this a vehicle counts as "em movimento". Below it (including a reported 0) it is
// stopped. A vehicle with no speed at all is neither: it is counted apart (missing is not zero).
export const MOVING_KMH = 3;

export const TOP_LINES = 8;

export type FleetStats = {
  total: number;
  bus: number;
  brt: number;
  // A mode the screen doesn't name yet is counted here instead of being dropped from the total.
  other: number;
  withSpeed: number;
  moving: number;
  noSpeed: number;
  // moving / withSpeed, 0..1; null when no vehicle reports a speed.
  movingShare: number | null;
  // Median speed of the moving vehicles only; null when none is moving.
  medianMovingKmh: number | null;
  // The lines with the most vehicles, most first; ties in natural line order. Vehicles with no line
  // are not a line and are not listed.
  topLines: { line: string; count: number }[];
};

export function median(sorted: number[]): number | null {
  if (sorted.length === 0) return null;
  const mid = Math.floor(sorted.length / 2);
  return sorted.length % 2 === 1 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2;
}

export function fleetStats(vehicles: Pick<Vehicle, "mode" | "line" | "speed_kmh">[]): FleetStats {
  let bus = 0;
  let brt = 0;
  let other = 0;
  let withSpeed = 0;
  let noSpeed = 0;
  const movingSpeeds: number[] = [];
  const perLine = new Map<string, number>();

  for (const v of vehicles) {
    if (v.mode === "bus") bus++;
    else if (v.mode === "brt") brt++;
    else other++;

    if (v.speed_kmh === null) {
      noSpeed++;
    } else {
      withSpeed++;
      if (v.speed_kmh >= MOVING_KMH) movingSpeeds.push(v.speed_kmh);
    }

    if (v.line !== null) perLine.set(v.line, (perLine.get(v.line) ?? 0) + 1);
  }

  movingSpeeds.sort((a, b) => a - b);
  const topLines = [...perLine]
    .map(([line, count]) => ({ line, count }))
    .sort((a, b) => b.count - a.count || compareLines(a.line, b.line))
    .slice(0, TOP_LINES);

  return {
    total: vehicles.length,
    bus,
    brt,
    other,
    withSpeed,
    moving: movingSpeeds.length,
    noSpeed,
    movingShare: withSpeed === 0 ? null : movingSpeeds.length / withSpeed,
    medianMovingKmh: median(movingSpeeds),
    topLines,
  };
}

const kmh = new Intl.NumberFormat("pt-BR", { minimumFractionDigits: 1, maximumFractionDigits: 1 });

// "18,0 km/h": one decimal always, so the number doesn't change width between refreshes.
export function formatKmh(value: number): string {
  return `${kmh.format(value)} km/h`;
}

export function formatPercent(share: number): string {
  return `${Math.round(share * 100)}%`;
}

// Module-level, so TanStack caches the result until the next response.
const selectFleetStats = (snapshot: Snapshot): FleetStats => fleetStats(snapshot.vehicles);

export function useFleetStats(): FleetStats | undefined {
  return useQuery({ ...snapshotQueryOptions(useDataSource()), select: selectFleetStats }).data;
}
