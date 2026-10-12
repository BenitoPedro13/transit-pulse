import { useLatestVehicles, type VehicleRow } from "./use-latest-vehicles";

// What the screen knows about the bus the URL names. "missing" is a real answer (the newest minute
// has no such bus), not an error and not a position: no older fix is ever shown as current.
export type SelectedVehicle =
  | { kind: "none" }
  | { kind: "loading" }
  | { kind: "unavailable" }
  | { kind: "found"; row: VehicleRow }
  | { kind: "missing"; asOfMinute: string };

export function findVehicle(rows: VehicleRow[], vehicle: string): VehicleRow | undefined {
  return rows.find((r) => r.vehicle === vehicle);
}

export function useSelectedVehicle(vehicle: string | null): SelectedVehicle {
  const { data, isError } = useLatestVehicles();
  if (vehicle === null) return { kind: "none" };
  if (!data) return isError ? { kind: "unavailable" } : { kind: "loading" };
  const row = findVehicle(data.rows, vehicle);
  return row ? { kind: "found", row } : { kind: "missing", asOfMinute: data.snapshot.as_of_minute };
}
