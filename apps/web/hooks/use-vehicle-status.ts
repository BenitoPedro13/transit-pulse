import { statusOf, useLatestVehicles, type Snapshot, type StatusKind } from "./use-latest-vehicles";
import { useNow } from "./use-now";

export type ScreenStatus = {
  kind: StatusKind | "loading" | "unavailable";
  message: string;
  snapshot: Snapshot | null;
};

// What the status strip shows: the snapshot's own status once there is one, and otherwise
// "loading" or "unavailable" (the API did not answer, which is not the same as "no vehicles").
export function useVehicleStatus(): ScreenStatus {
  const { data, isError } = useLatestVehicles();
  const now = useNow();
  if (data && now > 0) return { ...statusOf(data.snapshot, now), snapshot: data.snapshot };
  if (isError) {
    return { kind: "unavailable", message: "Não foi possível carregar os veículos. Tentando de novo.", snapshot: null };
  }
  return { kind: "loading", message: "Carregando veículos…", snapshot: null };
}
