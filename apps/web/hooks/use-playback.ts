import { useDataSource } from "./data-source";
import { useEffect } from "react";
import { useStore } from "zustand";
import { usePlaybackStore } from "@/store/playback-context";
import type { Speed } from "@/store/playback";
import {
  clampPlayhead,
  MINUTE_MS,
  minuteAt,
  minuteStart,
  playheadBounds,
  replayRows,
  type HistoryWindow,
  type MinuteSample,
  type ReplayRow,
} from "./history-window";
import { useHistoryWindow } from "./use-history";
import { useLatestVehicles } from "./use-latest-vehicles";
import { useReducedMotion } from "./use-reduced-motion";

// The selector hooks over the playback store (store/playback.ts). Nothing else calls useStore. The ones
// that change every frame are named for it; the rest change at most once a minute of playback, so the
// dashboard, the sign and the status strip stay out of the frame loop.

export function usePlaybackMode(): "live" | "replay" {
  return useStore(usePlaybackStore(), (s) => s.mode);
}

export function usePlaying(): boolean {
  return useStore(usePlaybackStore(), (s) => s.playing);
}

export function useSpeed(): Speed {
  return useStore(usePlaybackStore(), (s) => s.speed);
}

// The reader's choice, or by default on unless they asked for reduced motion.
export function useSmoothing(): boolean {
  const reduced = useReducedMotion();
  return useStore(usePlaybackStore(), (s) => s.smoothing) ?? !reduced;
}

// Every animation frame while playing. Only the map scene calls this.
export function usePlayhead(): number {
  return useStore(usePlaybackStore(), (s) => s.playhead);
}

// The minute the playhead is in (its start, epoch ms): changes once a minute of playback.
function usePlayheadMinute(): number {
  return useStore(usePlaybackStore(), (s) => minuteStart(s.playhead));
}

export type ReplayView =
  | { kind: "live" }
  | { kind: "loading" }
  | { kind: "unavailable" }
  | { kind: "ready"; window: HistoryWindow; minute: MinuteSample; playheadMs: number };

const LIVE: ReplayView = { kind: "live" };
const LOADING: ReplayView = { kind: "loading" };
const UNAVAILABLE: ReplayView = { kind: "unavailable" };

// What the screen shows around the map, to the minute: live, replay waiting for the history, replay
// without it (the API did not answer), or replay at a minute of the window.
export function useReplayView(): ReplayView {
  const mode = usePlaybackMode();
  const history = useHistoryWindow();
  const minuteMs = usePlayheadMinute();
  if (mode === "live") return LIVE;
  if (history.kind !== "ready") return history.kind === "loading" ? LOADING : UNAVAILABLE;
  const w = history.window;
  const playheadMs = clampPlayhead(w, minuteMs + MINUTE_MS);
  const minute = minuteAt(w, playheadMs) ?? w.minutes[w.minutes.length - 1];
  return { kind: "ready", window: w, minute, playheadMs };
}

// The vehicles at that minute, for the card and the panel (the map draws its own, per frame).
export function useReplayRows(view: ReplayView): ReplayRow[] | null {
  const smoothing = useSmoothing();
  const { data } = useLatestVehicles();
  if (view.kind === "live") return null;
  if (view.kind !== "ready") return NO_ROWS;
  return replayRows(view.window, view.playheadMs, smoothing, data?.labels ?? NO_LABELS);
}

const NO_ROWS: ReplayRow[] = [];
const NO_LABELS = new Map<string, string>();

export type ReplayFrame = {
  rows: ReplayRow[];
  // null while the history is still loading or unavailable: the map shows nobody, never the live fleet.
  window: HistoryWindow | null;
  trails: boolean;
  // Seconds since window.originMs, for TripsLayer.
  currentTime: number;
};

const EMPTY_FRAME: ReplayFrame = { rows: NO_ROWS, window: null, trails: false, currentTime: 0 };

// The map scene's per-frame view of the replay; null in live mode.
export function useReplayFrame(): ReplayFrame | null {
  const mode = usePlaybackMode();
  const history = useHistoryWindow();
  const playhead = usePlayhead();
  const smoothing = useSmoothing();
  const reduced = useReducedMotion();
  const { data } = useLatestVehicles();
  if (mode === "live") return null;
  if (history.kind !== "ready") return EMPTY_FRAME;
  const w = history.window;
  const p = clampPlayhead(w, playhead);
  return {
    rows: replayRows(w, p, smoothing, data?.labels ?? NO_LABELS),
    window: w,
    // Trails are motion: smoothing on and no reduced-motion preference.
    trails: smoothing && !reduced,
    currentTime: (p - w.originMs) / 1000,
  };
}

// What the timeline needs: the state, and every action with the window's bounds already applied.
export function usePlaybackControls() {
  const replayOnly = useDataSource().kind === 'replay';
  const store = usePlaybackStore();
  const history = useHistoryWindow();
  const mode = usePlaybackMode();
  const playing = usePlaying();
  const speed = useSpeed();
  const smoothing = useSmoothing();
  const minuteMs = usePlayheadMinute();
  const actions = store.getState().actions;

  const window = history.kind === "ready" ? history.window : null;
  const bounds = window ? playheadBounds(window) : null;

  // The scrubber works in minutes of the window. The last one is "now": live.
  const lastIndex = window ? window.minutes.length - 1 : 0;
  const index = !window ? 0 : mode === "live" ? lastIndex : Math.min(lastIndex, Math.max(0, (minuteMs - window.firstMinuteMs) / MINUTE_MS));

  return {
    replayOnly,
    ready: window !== null,
    status: history.kind,
    window,
    mode,
    playing,
    speed,
    smoothing,
    index,
    lastIndex,
    togglePlay: () => {
      if (!bounds) return;
      if (playing) actions.pause();
      else actions.play(bounds);
    },
    seekIndex: (i: number) => {
      if (!window) return;
      if (i >= lastIndex && !replayOnly) actions.goLive();
      else actions.seek(window.firstMinuteMs + i * MINUTE_MS + MINUTE_MS);
    },
    goLive: () => {
      if (replayOnly) { if (bounds) actions.seek(bounds.firstMs); }
      else actions.goLive();
    },
    setSpeed: actions.setSpeed,
    setSmoothing: actions.setSmoothing,
  };
}

// Drives the playhead while playing. A real Effect: the browser's frame clock is the external system.
//   normally:       every animation frame, the playhead moves by the real time that passed times the speed;
//   reduced motion: once a second, one whole minute.
// A hidden tab pauses. Render <PlaybackClock/> once; it draws nothing.
export function usePlaybackClock() {
  const store = usePlaybackStore();
  const history = useHistoryWindow();
  const playing = usePlaying();
  const speed = useSpeed();
  const reduced = useReducedMotion();
  const mode = usePlaybackMode();

  const bounds = history.kind === "ready" ? playheadBounds(history.window) : null;
  const firstMs = bounds?.firstMs ?? null;
  const lastMs = bounds?.lastMs ?? null;

  // A shared minute outside the window, or an hour that left it, is pulled back inside.
  useEffect(() => {
    if (firstMs === null || lastMs === null || mode !== "replay") return;
    store.getState().actions.clamp({ firstMs, lastMs });
  }, [store, mode, firstMs, lastMs]);

  useEffect(() => {
    if (!playing || firstMs === null || lastMs === null) return;
    const actions = store.getState().actions;
    const b = { firstMs, lastMs };

    const onVisibility = () => {
      if (document.hidden) actions.pause();
    };
    document.addEventListener("visibilitychange", onVisibility);

    if (reduced) {
      const id = setInterval(() => actions.stepMinute(b), 1000);
      return () => {
        clearInterval(id);
        document.removeEventListener("visibilitychange", onVisibility);
      };
    }

    let frame = 0;
    let last = performance.now();
    const tick = (now: number) => {
      // A long frame (a busy tab) does not make the playhead jump: at most a quarter second of real time.
      actions.advance(Math.min(now - last, 250) * speed, b);
      last = now;
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(frame);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [store, playing, speed, reduced, firstMs, lastMs]);
}
