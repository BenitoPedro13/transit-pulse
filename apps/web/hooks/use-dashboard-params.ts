"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { parseParams, withParams, type ColorBy, type DashboardParams } from "./dashboard-url";

export type DashboardState = DashboardParams & {
  // Choosing a line drops a bus that belongs to another line's selection.
  selectLine: (line: string) => void;
  clearLine: () => void;
  selectBus: (vehicle: string) => void;
  clearBus: () => void;
  setLinha: (line: string | null) => void;
  setCor: (cor: ColorBy) => void;
  setT: (t: string | null) => void;
};

// The one place that reads and writes the dashboard's URL. Must render under a <Suspense>:
// useSearchParams makes the tree up to the nearest boundary render in the browser.
export function useDashboardParams(): DashboardState {
  const search = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const params = parseParams(search);

  // replace, not push: picking a line is not a page to go back to, and no scroll jump.
  function update(patch: Partial<DashboardParams>) {
    const query = withParams(search.toString(), patch);
    router.replace(query ? `${pathname}?${query}` : pathname, { scroll: false });
  }

  return {
    ...params,
    selectLine: (line) => update({ linha: line, onibus: null }),
    clearLine: () => update({ linha: null }),
    selectBus: (vehicle) => update({ onibus: vehicle }),
    clearBus: () => update({ onibus: null }),
    setLinha: (line) => update({ linha: line }),
    setCor: (cor) => update({ cor }),
    setT: (t) => update({ t }),
  };
}
