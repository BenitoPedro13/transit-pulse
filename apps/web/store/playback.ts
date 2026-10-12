import { createStore } from "zustand/vanilla";
import { MINUTE_MS, type PlayheadBounds } from "@/hooks/history-window";

// The time machine's playback state (docs/tasks/TASK-time-machine.md §2.3). The playhead changes every
// animation frame while playing, and the map, the scrubber and the sign all read it without any other
// reason to be co-located: the one case the global rules keep a client-state library for.
//
// The playhead is a moment in epoch ms, in "as of" terms: minute M sits at M + 60 s (history-window.ts).
// In live mode it is unused: live shows the newest snapshot.
export type Speed = 1 | 10 | 60;
export const SPEEDS: readonly Speed[] = [1, 10, 60];

export type PlaybackState = {
  mode: "live" | "replay";
  playhead: number;
  playing: boolean;
  speed: Speed;
  // null means "the default", which depends on prefers-reduced-motion; true/false is the reader's choice.
  smoothing: boolean | null;
};

// The transitions, as pure functions of the state, so they can be tested without a store or a clock.

// Scrubbing, or opening a shared minute: replay at that moment, paused.
export function seek(s: PlaybackState, playhead: number): PlaybackState {
  return { ...s, mode: "replay", playhead, playing: false };
}

export function goLive(s: PlaybackState): PlaybackState {
  return { ...s, mode: "live", playing: false };
}

// From live, or from the newest minute, play starts at the window's first minute.
export function play(s: PlaybackState, b: PlayheadBounds): PlaybackState {
  if (s.mode === "live" || s.playhead >= b.lastMs) return { ...s, mode: "replay", playhead: b.firstMs, playing: true };
  return { ...s, playing: true };
}

export function pause(s: PlaybackState): PlaybackState {
  return s.playing ? { ...s, playing: false } : s;
}

// The animation clock: moves the playhead by `ms` of recorded time. Reaching the newest minute stops
// playing and goes live by itself.
export function advance(s: PlaybackState, ms: number, b: PlayheadBounds): PlaybackState {
  if (!s.playing || s.mode !== "replay") return s;
  const next = s.playhead + ms;
  if (next >= b.lastMs) return { ...s, mode: "live", playing: false, playhead: b.lastMs };
  return { ...s, playhead: next };
}

// The clock for readers who asked for no motion: one whole minute per tick.
export function stepMinute(s: PlaybackState, b: PlayheadBounds): PlaybackState {
  return advance(s, (Math.floor(s.playhead / MINUTE_MS) + 1) * MINUTE_MS - s.playhead, b);
}

// The window moved (an hour left it, or the shared minute was outside it): keep the playhead inside.
export function clamp(s: PlaybackState, b: PlayheadBounds): PlaybackState {
  if (s.mode !== "replay") return s;
  const playhead = Math.min(b.lastMs, Math.max(b.firstMs, s.playhead));
  return playhead === s.playhead ? s : { ...s, playhead };
}

export type PlaybackActions = {
  seek: (playhead: number) => void;
  goLive: () => void;
  play: (b: PlayheadBounds) => void;
  pause: () => void;
  advance: (ms: number, b: PlayheadBounds) => void;
  stepMinute: (b: PlayheadBounds) => void;
  clamp: (b: PlayheadBounds) => void;
  setSpeed: (speed: Speed) => void;
  setSmoothing: (smoothing: boolean) => void;
};

export type PlaybackStoreState = PlaybackState & { actions: PlaybackActions };
export type PlaybackStore = ReturnType<typeof createPlaybackStore>;

// A new store per mount (the provider makes it), never a module-level one: on the server that would be
// shared by every request. `startAt` is a playhead from the URL, or null for live.
export function createPlaybackStore(startAt: number | null) {
  return createStore<PlaybackStoreState>()((set) => {
    // One tiny wrapper per transition: apply it to the state, and keep the same object when nothing changed.
    const apply = (f: (s: PlaybackState) => PlaybackState) =>
      set((s) => {
        const next = f(s);
        return next === s ? s : next;
      });
    return {
      mode: startAt === null ? "live" : "replay",
      playhead: startAt ?? 0,
      playing: false,
      speed: 10,
      smoothing: null,
      actions: {
        seek: (playhead) => apply((s) => seek(s, playhead)),
        goLive: () => apply(goLive),
        play: (b) => apply((s) => play(s, b)),
        pause: () => apply(pause),
        advance: (ms, b) => apply((s) => advance(s, ms, b)),
        stepMinute: (b) => apply((s) => stepMinute(s, b)),
        clamp: (b) => apply((s) => clamp(s, b)),
        setSpeed: (speed) => set({ speed }),
        setSmoothing: (smoothing) => set({ smoothing }),
      },
    };
  });
}
