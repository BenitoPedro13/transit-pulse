import { createContext, useContext } from "react";
import type { PlaybackStore } from "./playback";

// The store is made per mount by components/playback-provider.tsx. Components never read this: they use
// the selector hooks in hooks/use-playback.ts.
export const PlaybackContext = createContext<PlaybackStore | null>(null);

export function usePlaybackStore(): PlaybackStore {
  const store = useContext(PlaybackContext);
  if (!store) throw new Error("usePlaybackStore needs a <PlaybackProvider> above it");
  return store;
}
