import { formatClock, formatCount, type StatusKind } from "./use-latest-vehicles";
import type { SelectedVehicle } from "./use-selected-vehicle";

export type LetreiroInput = {
  // The data's state from statusOf: it decides the prefix. "loading"/"unavailable" come from before
  // there is a snapshot.
  status: StatusKind | "loading" | "unavailable";
  asOfMinute: string | null;
  total: number;
  linha: string | null;
  lineCount: number;
  selected: SelectedVehicle;
  onibus: string | null;
  timeZone?: string;
};

// What the sign says, in capitals like a real one. In order of precedence:
//   a bus is selected:  "474 · A29001"  (or "<id> · NÃO APARECE" when the newest minute lacks it)
//   a line is selected: "474 · 31 ÔNIBUS"
//   nothing selected:   "13:24 · 2.627 ÔNIBUS NA RUA"
// When the data isn't live the text starts with "ATRASADO" (stale) or "SEM SINAL" (failed), so the sign
// never looks live when it isn't. Before the first response it says "CARREGANDO"; if the API doesn't
// answer, "SEM SINAL".
export function letreiroText(input: LetreiroInput): string {
  if (input.status === "loading") return "CARREGANDO";
  if (input.status === "unavailable") return "SEM SINAL";

  let text: string;
  if (input.onibus !== null) {
    text =
      input.selected.kind === "found"
        ? `${input.selected.row.line ?? "SEM LINHA"} · ${input.onibus}`
        : `${input.onibus} · NÃO APARECE`;
  } else if (input.linha !== null) {
    text = `${input.linha} · ${formatCount(input.lineCount)} ÔNIBUS`;
  } else {
    const clock = input.asOfMinute ? formatClock(input.asOfMinute, input.timeZone) : "--:--";
    text = `${clock} · ${formatCount(input.total)} ÔNIBUS NA RUA`;
  }

  const prefix = input.status === "failed" ? "SEM SINAL" : input.status === "stale" ? "ATRASADO" : "";
  return (prefix ? `${prefix} · ${text}` : text).toLocaleUpperCase("pt-BR");
}
