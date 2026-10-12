"use client";

import { useEffect } from "react";
import { formatT } from "@/hooks/dashboard-url";
import { useDashboardParams } from "@/hooks/use-dashboard-params";
import { usePlaybackMode, usePlaying, useReplayView } from "@/hooks/use-playback";

// Keeps ?t= equal to the minute a paused replay is on, and absent while live or playing, so a link copied
// from a paused replay opens on the same minute. A real Effect: the URL (the router) is the external system.
// The other direction, opening a link with ?t=, is done once by the provider.
export default function PlaybackUrlSync() {
  const params = useDashboardParams();
  const mode = usePlaybackMode();
  const playing = usePlaying();
  const view = useReplayView();

  const wanted = mode === "replay" && !playing && view.kind === "ready" ? formatT(view.minute.startMs) : mode === "replay" && view.kind !== "ready" ? params.t : null;

  useEffect(() => {
    if (wanted !== params.t) params.setT(wanted);
    // params.setT closes over the current search; only the two values matter.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wanted, params.t]);

  return null;
}
