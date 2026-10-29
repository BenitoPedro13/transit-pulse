import { useDataSource } from "./data-source";
import { useQuery } from "@tanstack/react-query";
import { snapshotQueryOptions, useLatestVehicles, type Snapshot, type Vehicle, type VehicleRow } from "./use-latest-vehicles";

export type LineEntry = {
  line: string;
  // The mode of the line's vehicles. A line's vehicles all share one mode in the recorded data
  // (asserted in the test); if that ever changes, the first vehicle's mode is shown.
  mode: string;
  count: number;
};

export type LineIndex = {
  lines: LineEntry[];
  // Vehicles with line: null. They are not hidden: the list ends with a sentence about them.
  unidentified: number;
};

// "007" -> ["007"], "1 GAR" -> ["1", " GAR"], "SP12" -> ["SP", "12"]
function chunks(line: string): string[] {
  return line.split(/(\d+)/).filter((c) => c !== "");
}

const isNumber = (chunk: string) => /^\d+$/.test(chunk);

// Natural order: "2" < "10" < "343" < "ESPECIAL". Numbers come before words, digits compare as numbers,
// words compare ignoring case. Ties (like "7" and "007") fall back to the raw text so the order is stable.
export function compareLines(a: string, b: string): number {
  const ca = chunks(a);
  const cb = chunks(b);
  for (let i = 0; i < Math.min(ca.length, cb.length); i++) {
    const x = ca[i];
    const y = cb[i];
    if (isNumber(x) && isNumber(y)) {
      const diff = Number(x) - Number(y);
      if (diff !== 0) return diff;
    } else if (isNumber(x)) {
      return -1;
    } else if (isNumber(y)) {
      return 1;
    } else {
      const lx = x.toLowerCase();
      const ly = y.toLowerCase();
      if (lx !== ly) return lx < ly ? -1 : 1;
    }
  }
  if (ca.length !== cb.length) return ca.length - cb.length;
  return a < b ? -1 : a > b ? 1 : 0;
}

export function toLineIndex(vehicles: Pick<Vehicle, "line" | "mode">[]): LineIndex {
  const byLine = new Map<string, LineEntry>();
  let unidentified = 0;
  for (const v of vehicles) {
    if (v.line === null) {
      unidentified++;
      continue;
    }
    const entry = byLine.get(v.line);
    if (entry) entry.count++;
    else byLine.set(v.line, { line: v.line, mode: v.mode, count: 1 });
  }
  return { lines: [...byLine.values()].sort((a, b) => compareLines(a.line, b.line)), unidentified };
}

// A prefix match, case-insensitive: "47" finds 474 and 475. An empty query keeps everything.
export function matchLines(lines: LineEntry[], query: string): LineEntry[] {
  const q = query.trim().toLowerCase();
  if (!q) return lines;
  return lines.filter((l) => l.line.toLowerCase().startsWith(q));
}

const selectLineIndex = (snapshot: Snapshot): LineIndex => toLineIndex(snapshot.vehicles);

export function useLineIndex(): LineIndex | undefined {
  return useQuery({ ...snapshotQueryOptions(useDataSource()), select: selectLineIndex }).data;
}

// One line's vehicles, ordered by vehicle ID (natural order).
export function vehiclesOfLine<T extends { line: string | null; vehicle: string }>(rows: T[], line: string): T[] {
  return rows.filter((r) => r.line === line).sort((a, b) => compareLines(a.vehicle, b.vehicle));
}

const NO_ROWS: VehicleRow[] = [];

export function useVehiclesOfLine(line: string | null): VehicleRow[] {
  const { data } = useLatestVehicles();
  return line === null || !data ? NO_ROWS : vehiclesOfLine(data.rows, line);
}
