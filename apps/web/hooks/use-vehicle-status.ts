import { formatClock, statusOf, useLatestVehicles, type Snapshot, type StatusKind } from "./use-latest-vehicles";
import { useNow } from "./use-now";
import { useReplayView } from "./use-playback";

export type ScreenStatus = {
  // "replay" is its own state: a replay is not live, and not stale or failed either.
  kind: StatusKind | "loading" | "unavailable" | "replay";
  message: string;
  snapshot: Snapshot | null;
};

// What the status strip shows: the snapshot's own status once there is one, and otherwise
// "loading" or "unavailable" (the API did not answer, which is not the same as "no vehicles").
export function useVehicleStatus(): ScreenStatus {
  const { data, isError } = useLatestVehicles();
  const now = useNow();
  const replay = useReplayView();
  if (replay.kind !== "live") {
    const date = new Intl.DateTimeFormat("pt-BR", { day: "2-digit", month: "2-digit" }).format(new Date(replay.kind === "ready" ? replay.minute.startMs : 0));
    const message =
      replay.kind === "ready"
        ? `Reprodução de ${date} ${formatClock(new Date(replay.minute.startMs).toISOString())} — não é ao vivo`
        : replay.kind === "loading"
          ? "Reprodução — carregando o histórico. Não é ao vivo"
          : "Reprodução — histórico indisponível. Não é ao vivo";
    return { kind: "replay", message, snapshot: data?.snapshot ?? null };
  }
  if (data && now > 0) return { ...statusOf(data.snapshot, now), snapshot: data.snapshot };
  if (isError) {
    return { kind: "unavailable", message: "Não foi possível carregar os veículos. Tentando de novo.", snapshot: null };
  }
  return { kind: "loading", message: "Carregando veículos…", snapshot: null };
}
