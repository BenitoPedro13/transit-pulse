import { useSyncExternalStore } from "react";
import { mediaQueryStore } from "./media-query";

export type ColorScheme = "light" | "dark";

const dark = mediaQueryStore("(prefers-color-scheme: dark)");

// The system colour scheme, live: the basemap and the vehicle colours follow it while the page is
// open. There is no manual toggle. Light on the server, which draws no map.
export function useColorScheme(): ColorScheme {
  return useSyncExternalStore(dark.subscribe, dark.getSnapshot, () => false) ? "dark" : "light";
}
