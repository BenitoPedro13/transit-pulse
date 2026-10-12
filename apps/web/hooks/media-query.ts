// A matchMedia query as an external store for useSyncExternalStore. Built once per query at module
// level, so subscribe keeps its identity and React doesn't resubscribe on every render.
export function mediaQueryStore(query: string) {
  return {
    subscribe(onChange: () => void) {
      const list = window.matchMedia(query);
      list.addEventListener("change", onChange);
      return () => list.removeEventListener("change", onChange);
    },
    getSnapshot: () => window.matchMedia(query).matches,
  };
}
