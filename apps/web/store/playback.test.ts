import { describe, expect, it } from "vitest";
import { MINUTE_MS, type PlayheadBounds } from "@/hooks/history-window";
import { advance, clamp, createPlaybackStore, goLive, pause, play, seek, stepMinute, type PlaybackState } from "./playback";

// The recorded run: minutes 14:27 to 16:24 UTC, so the playhead runs from the end of the first minute to
// the end of the last.
const FIRST = Date.parse("2026-10-12T14:28:00Z");
const LAST = Date.parse("2026-10-12T16:25:00Z");
const BOUNDS: PlayheadBounds = { firstMs: FIRST, lastMs: LAST };

const live: PlaybackState = { mode: "live", playhead: 0, playing: false, speed: 10, smoothing: null };

describe("play", () => {
  it("from live starts at the window's first minute", () => {
    const s = play(live, BOUNDS);
    expect(s).toMatchObject({ mode: "replay", playing: true, playhead: FIRST });
  });

  it("from a paused minute keeps the minute", () => {
    const paused = seek(live, FIRST + 30 * MINUTE_MS);
    expect(play(paused, BOUNDS)).toMatchObject({ mode: "replay", playing: true, playhead: FIRST + 30 * MINUTE_MS });
  });

  it("from the newest minute starts over", () => {
    expect(play(seek(live, LAST), BOUNDS).playhead).toBe(FIRST);
  });
});

describe("advance", () => {
  it("moves the playhead by the recorded time that passed", () => {
    const s = advance(play(live, BOUNDS), 5 * MINUTE_MS, BOUNDS);
    expect(s.playhead).toBe(FIRST + 5 * MINUTE_MS);
    expect(s.playing).toBe(true);
  });

  it("does nothing while paused or live", () => {
    const paused = seek(live, FIRST);
    expect(advance(paused, MINUTE_MS, BOUNDS)).toBe(paused);
    expect(advance(live, MINUTE_MS, BOUNDS)).toBe(live);
  });

  it("goes live by itself on reaching the newest minute", () => {
    const near = { ...play(live, BOUNDS), playhead: LAST - 1000 };
    const s = advance(near, 5000, BOUNDS);
    expect(s).toMatchObject({ mode: "live", playing: false, playhead: LAST });
  });

  it("at 60x from the window's start reaches the newest minute in about two minutes of real time", () => {
    // 16.7 ms frames, as a 60 Hz display delivers them (T6's arithmetic; the screen run measures it).
    let s = play({ ...live, speed: 60 }, BOUNDS);
    let real = 0;
    while (s.mode === "replay") {
      s = advance(s, 16.7 * 60, BOUNDS);
      real += 16.7;
    }
    expect(real / 1000).toBeGreaterThan(115);
    expect(real / 1000).toBeLessThan(123);
    expect(s.playing).toBe(false);
  });
});

describe("pause, seek, live", () => {
  it("scrubbing while playing pauses and stays in replay", () => {
    const s = seek(advance(play(live, BOUNDS), MINUTE_MS, BOUNDS), FIRST + 10 * MINUTE_MS);
    expect(s).toMatchObject({ mode: "replay", playing: false, playhead: FIRST + 10 * MINUTE_MS });
  });

  it("pause keeps the playhead, and is the same object when already paused", () => {
    const playing = play(live, BOUNDS);
    expect(pause(playing)).toMatchObject({ playing: false, playhead: FIRST });
    const paused = pause(playing);
    expect(pause(paused)).toBe(paused);
  });

  it("goLive stops playing", () => {
    expect(goLive(play(live, BOUNDS))).toMatchObject({ mode: "live", playing: false });
  });
});

describe("stepMinute (reduced motion)", () => {
  it("advances exactly one whole minute, from a moment inside a minute too", () => {
    const s = stepMinute({ ...play(live, BOUNDS), playhead: FIRST + 20_000 }, BOUNDS);
    expect(s.playhead).toBe(FIRST + MINUTE_MS);
  });
});

describe("clamp", () => {
  it("pulls a shared minute outside the window to the nearest end", () => {
    expect(clamp(seek(live, FIRST - 10 * MINUTE_MS), BOUNDS).playhead).toBe(FIRST);
    expect(clamp(seek(live, LAST + MINUTE_MS), BOUNDS).playhead).toBe(LAST);
  });

  it("leaves a minute inside the window, and live, alone", () => {
    const inside = seek(live, FIRST + MINUTE_MS);
    expect(clamp(inside, BOUNDS)).toBe(inside);
    expect(clamp(live, BOUNDS)).toBe(live);
  });
});

describe("createPlaybackStore", () => {
  it("starts live without a shared minute, and in replay (paused) with one", () => {
    expect(createPlaybackStore(null).getState()).toMatchObject({ mode: "live", playing: false });
    expect(createPlaybackStore(FIRST).getState()).toMatchObject({ mode: "replay", playing: false, playhead: FIRST });
  });

  it("applies the transitions to its own state", () => {
    const store = createPlaybackStore(null);
    store.getState().actions.play(BOUNDS);
    store.getState().actions.advance(MINUTE_MS, BOUNDS);
    expect(store.getState()).toMatchObject({ mode: "replay", playing: true, playhead: FIRST + MINUTE_MS });
    store.getState().actions.setSpeed(60);
    store.getState().actions.setSmoothing(false);
    expect(store.getState()).toMatchObject({ speed: 60, smoothing: false });
  });

  it("does not share state between two stores", () => {
    const a = createPlaybackStore(null);
    const b = createPlaybackStore(null);
    a.getState().actions.play(BOUNDS);
    expect(b.getState().mode).toBe("live");
  });

  it("does not notify when a transition changes nothing", () => {
    const store = createPlaybackStore(null);
    let calls = 0;
    store.subscribe(() => calls++);
    store.getState().actions.pause();
    store.getState().actions.advance(MINUTE_MS, BOUNDS);
    expect(calls).toBe(0);
  });
});

describe("packaged replay", () => {
  it("never becomes live when playback reaches the end or goLive is requested", () => {
    const store = createPlaybackStore(null, true);
    const actions = store.getState().actions;
    expect(store.getState().mode).toBe("replay");
    actions.clamp(BOUNDS);
    actions.play(BOUNDS);
    actions.advance(BOUNDS.lastMs - BOUNDS.firstMs + 1, BOUNDS);
    expect(store.getState()).toMatchObject({ mode: "replay", playing: false, playhead: BOUNDS.lastMs });
    actions.goLive();
    expect(store.getState().mode).toBe("replay");
    actions.play(BOUNDS);
    expect(store.getState()).toMatchObject({ mode: "replay", playing: true, playhead: BOUNDS.firstMs });
  });
});
