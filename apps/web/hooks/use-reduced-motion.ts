import { useSyncExternalStore } from "react";
import { mediaQueryStore } from "./media-query";

const reduced = mediaQueryStore("(prefers-reduced-motion: reduce)");

// True when the reader asked for no motion: no letreiro roll, and the camera jumps instead of easing.
export function useReducedMotion(): boolean {
  return useSyncExternalStore(reduced.subscribe, reduced.getSnapshot, () => false);
}
