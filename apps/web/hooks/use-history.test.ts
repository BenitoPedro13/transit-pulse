import { describe, expect, it } from "vitest";
import hour15 from "./testdata/history-hour-15.json";
import hour16 from "./testdata/history-hour-16.json";
import { combineHours, type HourEntry } from "./use-history";
import type { HourFile } from "./history-window";

const loaded = (file: HourFile) => ({ data: { file, etag: file.hour_utc }, isPending: false, isError: false });
const pending = { data: undefined as HourEntry | undefined, isPending: true, isError: false };
const failed = { data: undefined as HourEntry | undefined, isPending: false, isError: true };
const first = hour15 as HourFile;
const last = hour16 as HourFile;

describe("history window readiness", () => {
  it("waits for the requested hour instead of exposing bounds from whichever file arrived first", () => {
    expect(combineHours([loaded(first), pending])).toEqual({ kind: "loading" });
    expect(combineHours([pending, loaded(last)])).toEqual({ kind: "loading" });

    const result = combineHours([loaded(first), loaded(last)]);
    expect(result.kind).toBe("ready");
    if (result.kind !== "ready") throw new Error("recorded history should be ready");
    expect(result.window.firstMinuteMs).toBe(Date.parse("2026-10-12T15:00Z"));
    expect(result.window.lastMinuteMs).toBe(Date.parse("2026-10-12T16:24Z"));
  });

  it("keeps loading while one hour is pending even if another has failed", () => {
    expect(combineHours([failed, pending])).toEqual({ kind: "loading" });
  });

  it("uses available real history once missing hours have settled as errors", () => {
    expect(combineHours([loaded(first), failed]).kind).toBe("ready");
    expect(combineHours([failed, loaded(last)]).kind).toBe("ready");
    expect(combineHours([failed, failed])).toEqual({ kind: "error" });
  });
});
