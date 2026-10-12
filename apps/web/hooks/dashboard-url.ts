// The dashboard's view lives in the URL: /?linha=474&onibus=A29001&cor=velocidade&t=2026-10-12T12:47Z.
// Pure functions here; use-dashboard-params.ts wires them to the router.
export type ColorBy = "modo" | "velocidade";

export type DashboardParams = {
  linha: string | null;
  onibus: string | null;
  cor: ColorBy;
  // A replay minute, "2026-10-12T12:47Z"; null is live. Only a well-formed minute is kept.
  t: string | null;
};

type Reader = { get(name: string): string | null };

// Anything unknown or empty falls back to the default: a mistyped URL shows the default view.
// An unknown line or bus is still a valid value; the screen says it isn't in the last minute.
export function parseParams(search: Reader): DashboardParams {
  const t = search.get("t");
  return {
    linha: search.get("linha")?.trim() || null,
    onibus: search.get("onibus")?.trim() || null,
    cor: search.get("cor") === "velocidade" ? "velocidade" : "modo",
    t: parseT(t) === null ? null : t,
  };
}

const T_FORMAT = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}Z$/;

// "2026-10-12T12:47Z" -> the start of that minute in epoch ms; anything else (a bad date, seconds, no Z) is null.
export function parseT(value: string | null): number | null {
  if (value === null || !T_FORMAT.test(value)) return null;
  const ms = Date.parse(value);
  return Number.isNaN(ms) ? null : ms;
}

export function formatT(minuteStartMs: number): string {
  return `${new Date(minuteStartMs).toISOString().slice(0, 16)}Z`;
}

// The query string (without "?") after applying a patch. Defaults are left out so the URL of the
// default view is just "/". Keys this module doesn't own are kept.
export function withParams(search: string, patch: Partial<DashboardParams>): string {
  const next = new URLSearchParams(search);
  const set = (key: string, value: string | null) => {
    if (value === null) next.delete(key);
    else next.set(key, value);
  };
  if ("linha" in patch) set("linha", patch.linha ?? null);
  if ("onibus" in patch) set("onibus", patch.onibus ?? null);
  if ("cor" in patch) set("cor", patch.cor === "velocidade" ? "velocidade" : null);
  if ("t" in patch) set("t", patch.t ?? null);
  return next.toString();
}
