"use client";

import { useSearchParams } from "next/navigation";
import { useState } from "react";
import { useDataSource } from "@/hooks/data-source";
import { MINUTE_MS } from "@/hooks/history-window";
import { parseT } from "@/hooks/dashboard-url";
import { createPlaybackStore } from "@/store/playback";
import { PlaybackContext } from "@/store/playback-context";

// One playback store per mount (never module-level: on the server that would be shared by every request).
// Opening a URL with ?t=... lands in replay at the end of that minute, paused. Must render under a
// <Suspense>, like everything that reads the URL.
export default function PlaybackProvider({ children }: { children: React.ReactNode }) {
  const search = useSearchParams();
  const source = useDataSource();
  const [store] = useState(() => {
    const minute = parseT(search.get("t"));
    return createPlaybackStore(minute === null ? null : minute + MINUTE_MS, source.kind === 'replay');
  });
  return <PlaybackContext value={store}>{children}</PlaybackContext>;
}
