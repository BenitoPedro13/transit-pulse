import { useSyncExternalStore } from "react";
import { mediaQueryStore } from "./media-query";

// Tailwind's `lg`. The layout itself switches with CSS; this only tells the map how much of it the
// panels cover, so a fitted selection stays visible.
const desktop = mediaQueryStore("(min-width: 1024px)");

export function useIsDesktop(): boolean {
  return useSyncExternalStore(desktop.subscribe, desktop.getSnapshot, () => true);
}
