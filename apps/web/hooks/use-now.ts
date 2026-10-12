import { useSyncExternalStore } from "react";

// The current time, refreshed every TICK_MS. Needed because "is this snapshot still live?"
// changes with the clock even when no new data arrives, and an unchanged query result does not
// re-render anything. The timer is the external system; useSyncExternalStore (not useState) keeps
// the clock out of the server render: Next.js refuses Date.now() during prerender, and the server
// has no use for it (there is no data to judge until the browser has fetched it).
const TICK_MS = 15_000;

let current = 0;

function subscribe(onChange: () => void) {
  current = Date.now();
  const id = setInterval(() => {
    current = Date.now();
    onChange();
  }, TICK_MS);
  return () => clearInterval(id);
}

function getSnapshot() {
  if (current === 0) current = Date.now(); // client only: the server uses getServerSnapshot
  return current;
}

// 0 means "no clock yet". Callers must not judge a snapshot's age with it.
export function useNow(): number {
  return useSyncExternalStore(subscribe, getSnapshot, () => 0);
}
