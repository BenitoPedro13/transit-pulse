"use client";

import { usePlaybackClock } from "@/hooks/use-playback";

// Runs the clock that moves the playhead. Draws nothing.
export default function PlaybackClock() {
  usePlaybackClock();
  return null;
}
