// The dashboard's view lives in the URL: /?linha=474&onibus=A29001&cor=velocidade. Pure functions
// here; use-dashboard-params.ts wires them to the router.
export type ColorBy = "modo" | "velocidade";

export type DashboardParams = {
  linha: string | null;
  onibus: string | null;
  cor: ColorBy;
};

type Reader = { get(name: string): string | null };

// Anything unknown or empty falls back to the default: a mistyped URL shows the default view.
// An unknown line or bus is still a valid value; the screen says it isn't in the last minute.
export function parseParams(search: Reader): DashboardParams {
  return {
    linha: search.get("linha")?.trim() || null,
    onibus: search.get("onibus")?.trim() || null,
    cor: search.get("cor") === "velocidade" ? "velocidade" : "modo",
  };
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
  return next.toString();
}
