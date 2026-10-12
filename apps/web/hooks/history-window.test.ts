import { describe, expect, it } from "vitest";
import hour15 from "./testdata/history-hour-15.json";
import hour16 from "./testdata/history-hour-16.json";
import {
  buildWindow,
  clampPlayhead,
  GAP_SECONDS,
  HistoryFormatError,
  isGapMinute,
  minuteAt,
  minuteText,
  MINUTE_MS,
  newestFixAtOrBefore,
  playheadBounds,
  replayRows,
  tripsData,
  type HistoryWindow,
  type HourFile,
} from "./history-window";

// The fixtures are two hour files of the recorded run (2026-10-12 15:00 and 16:00 UTC, as GET
// /v1/history/{hour} served them), trimmed to 20 vehicles chosen for what they do: clean BRT and buses,
// gaps just over and far over 300 s, line changes, no line at all, and vehicles seen only briefly. Every
// fix, minute and feed count is real. Hour 16 ends at 16:24 (the recording's last minute).
// The recording has no null speed and no failed minute, so those two rules are tested on a copy of a
// real file with that one value changed, said where it happens.
const files = [hour15, hour16] as unknown as HourFile[];
const HOUR15_MS = Date.parse("2026-10-12T15:00:00Z");
const NO_LABELS = new Map<string, string>();

function clone(f: HourFile): HourFile {
  return structuredClone(f);
}

// The oracle for paths and fixes: straight from the files, with plain Maps and no typed arrays.
function perVehicle(): Map<string, { t: number; lon: number; lat: number; spd: number; line: string | null }[]> {
  const out = new Map<string, { t: number; lon: number; lat: number; spd: number; line: string | null }[]>();
  for (const f of files) {
    const base = Date.parse(f.hour_utc) / 1000;
    f.fixes.v.forEach((e, i) => {
      const v = f.vehicles[e];
      const key = `${v.operator}|${v.vehicle}`;
      const list = out.get(key) ?? [];
      list.push({ t: base + f.fixes.t[i], lon: f.fixes.lon[i] / 1e5, lat: f.fixes.lat[i] / 1e5, spd: f.fixes.spd[i], line: v.line });
      out.set(key, list);
    });
  }
  for (const list of out.values()) list.sort((a, b) => a.t - b.t);
  return out;
}

function windowOf(fs: HourFile[]): HistoryWindow {
  const w = buildWindow(fs);
  if (!w) throw new Error("empty window");
  return w;
}

function seriesOf(w: HistoryWindow, key: string): number {
  const [operator, vehicle] = key.split("|");
  const s = w.series.findIndex((x) => x.operator === operator && x.vehicle === vehicle);
  if (s < 0) throw new Error(`${key} is not in the fixture`);
  return s;
}

describe("buildWindow", () => {
  const w = windowOf(files);

  it("covers the recorded minutes and trims the minutes after the newest one", () => {
    expect(w.originMs).toBe(HOUR15_MS);
    expect(w.firstMinuteMs).toBe(HOUR15_MS);
    expect(w.lastMinuteMs).toBe(Date.parse("2026-10-12T16:24:00Z"));
    // 15:00 to 16:24 inclusive: hour 16's later minutes are `missing = 4` and are not part of the window.
    expect(w.minutes).toHaveLength(60 + 25);
    expect(w.minutes[0].vehicles).toBe(hour15.minutes[0].vehicles);
    expect(w.minutes.at(-1)?.vehicles).toBe(hour16.minutes[24].vehicles);
  });

  it("holds every fix of every vehicle, in time order, with the values of the file", () => {
    const oracle = perVehicle();
    expect(w.series).toHaveLength(oracle.size);
    expect(w.t.length).toBe(hour15.fixes.v.length + hour16.fixes.v.length);
    for (const [key, fixes] of oracle) {
      const s = seriesOf(w, key);
      expect(w.starts[s + 1] - w.starts[s]).toBe(fixes.length);
      fixes.forEach((fix, i) => {
        const k = w.starts[s] + i;
        expect(w.originMs / 1000 + w.t[k]).toBe(fix.t);
        expect(w.xy[2 * k]).toBe(fix.lon);
        expect(w.xy[2 * k + 1]).toBe(fix.lat);
        expect(w.spd[k]).toBe(fix.spd);
        expect(w.lines[w.line[k]]).toBe(fix.line);
      });
    }
  });

  it("keeps a vehicle's fixes in one series across the hour boundary and across a line change", () => {
    const oracle = perVehicle();
    const changed = [...oracle].filter(([, f]) => new Set(f.map((x) => x.line)).size > 1);
    expect(changed.length).toBeGreaterThan(0);
    for (const [key, fixes] of changed) {
      const s = seriesOf(w, key);
      expect(w.starts[s + 1] - w.starts[s]).toBe(fixes.length);
    }
  });

  it("rejects a file that breaks the contract", () => {
    const short = clone(hour15 as unknown as HourFile);
    short.minutes.pop();
    expect(() => buildWindow([short])).toThrow(HistoryFormatError);
    const ragged = clone(hour15 as unknown as HourFile);
    ragged.fixes.spd.pop();
    expect(() => buildWindow([ragged])).toThrow(HistoryFormatError);
  });

  it("is null when no minute was recorded", () => {
    const empty = clone(hour15 as unknown as HourFile);
    empty.minutes = empty.minutes.map(() => ({ vehicles: 0, feeds: { ok: 0, failed: 0, provider_not_ok: 0, gap: 0, missing: 4 } }));
    expect(buildWindow([empty])).toBeNull();
  });

  it("trims the lead-in before the recording started", () => {
    // A copy of the real hour 15 whose first ten minutes were never recorded.
    const late = clone(hour15 as unknown as HourFile);
    for (let i = 0; i < 10; i++) late.minutes[i] = { vehicles: 0, feeds: { ok: 0, failed: 0, provider_not_ok: 0, gap: 0, missing: 4 } };
    const lw = windowOf([late, hour16 as unknown as HourFile]);
    expect(lw.firstMinuteMs).toBe(HOUR15_MS + 10 * MINUTE_MS);
    expect(lw.minutes).toHaveLength(75);
    // Time stays relative to the first hour, so the paths do not move.
    expect(lw.originMs).toBe(HOUR15_MS);
  });

  it("fills an hour that failed to load with gaps instead of closing the hole", () => {
    // Only hours 15 and a copy of 16 renamed to 17: 16:00 to 16:59 is absent.
    const seventeen = clone(hour16 as unknown as HourFile);
    seventeen.hour_utc = "2026-10-12T17:00:00Z";
    const hw = windowOf([hour15 as unknown as HourFile, seventeen]);
    expect(hw.minutes).toHaveLength(60 + 60 + 25);
    const hole = hw.minutes[60 + 30];
    expect(hole.gap).toBe(true);
    expect(hole.startMs).toBe(Date.parse("2026-10-12T16:30:00Z"));
  });
});

describe("gap minutes", () => {
  it("is a gap when no feed reported anything, and not when only some failed", () => {
    expect(isGapMinute({ ok: 0, failed: 4, provider_not_ok: 0, gap: 0, missing: 0 })).toBe(true);
    expect(isGapMinute({ ok: 0, failed: 0, provider_not_ok: 0, gap: 0, missing: 4 })).toBe(true);
    expect(isGapMinute({ ok: 0, failed: 3, provider_not_ok: 1, gap: 0, missing: 0 })).toBe(false);
    expect(isGapMinute({ ok: 3, failed: 1, provider_not_ok: 0, gap: 0, missing: 0 })).toBe(false);
  });

  it("marks a failed minute inside the window as a gap, and a real minute as not", () => {
    // A copy of real hour 15 where the feeds of 15:30 all failed (the recording has no such minute).
    const failed = clone(hour15 as unknown as HourFile);
    failed.minutes[30] = { vehicles: 0, feeds: { ok: 0, failed: 4, provider_not_ok: 0, gap: 0, missing: 0 } };
    const w = windowOf([failed, hour16 as unknown as HourFile]);
    expect(w.minutes[30].gap).toBe(true);
    expect(w.minutes[29].gap).toBe(false);
    expect(w.minutes[31].gap).toBe(false);
    // It stays inside the window, so the chart draws a hole there instead of dropping the minute.
    expect(w.minutes).toHaveLength(85);
  });

  it("says what a minute is in words", () => {
    const count = (n: number) => String(n);
    const ok = { startMs: 0, vehicles: 2598, feeds: { ok: 4, failed: 0, provider_not_ok: 0, gap: 0, missing: 0 }, gap: false, feedsTotal: 4 };
    expect(minuteText(ok, "12:47", count)).toBe("12:47, 2598 ônibus");
    expect(minuteText({ ...ok, feeds: { ...ok.feeds, ok: 3, failed: 1 } }, "12:47", count)).toBe("12:47, 2598 ônibus, 3 de 4 fontes");
    expect(minuteText({ ...ok, vehicles: 0, gap: true, feeds: { ok: 0, failed: 4, provider_not_ok: 0, gap: 0, missing: 0 } }, "12:47", count)).toBe(
      "12:47, falha na coleta",
    );
  });
});

describe("paths", () => {
  const w = windowOf(files);

  it("splits a vehicle's fixes exactly where two consecutive ones are more than 300 s apart", () => {
    const oracle = perVehicle();
    let sawGap = false;
    for (const [key, fixes] of oracle) {
      const gaps = fixes.slice(1).filter((f, i) => f.t - fixes[i].t > GAP_SECONDS).length;
      sawGap ||= gaps > 0;
      const s = seriesOf(w, key);
      const paths = [...w.pathSeries].filter((p) => p === s).length;
      expect(paths, key).toBe(gaps + 1);
    }
    expect(sawGap).toBe(true);
  });

  it("never bridges a gap: no path has two consecutive fixes more than 300 s apart", () => {
    for (let p = 0; p < w.pathSeries.length; p++) {
      const end = p + 1 < w.pathStarts.length ? w.pathStarts[p + 1] : w.t.length;
      for (let k = w.pathStarts[p] + 1; k < end; k++) expect(w.t[k] - w.t[k - 1]).toBeLessThanOrEqual(GAP_SECONDS);
    }
  });

  it("starts a path at every series start, so one path never mixes two vehicles", () => {
    for (let s = 0; s < w.series.length; s++) expect(w.pathStarts).toContain(w.starts[s]);
  });

  it("hands TripsLayer binary attributes of matching length", () => {
    const d = tripsData(w);
    expect(d.length).toBe(w.pathSeries.length);
    expect(d.startIndices).toHaveLength(d.length);
    expect(d.attributes.getPath.value).toHaveLength(2 * d.attributes.getTimestamps.value.length);
  });
});

describe("replayRows", () => {
  const w = windowOf(files);
  const oracle = perVehicle();
  const at = (iso: string) => Date.parse(iso);

  // A BRT vehicle with two consecutive fixes under 300 s apart, and one with a 323 s gap.
  const clean = "rio-brt|7002";
  const gappy = "rio-brt|902091";

  function consecutive(key: string, wantGap: boolean) {
    const fixes = oracle.get(key) ?? [];
    for (let i = 1; i < fixes.length; i++) {
      if ((fixes[i].t - fixes[i - 1].t > GAP_SECONDS) === wantGap) return [fixes[i - 1], fixes[i]] as const;
    }
    throw new Error(`${key} has no ${wantGap ? "gap" : "pair"}`);
  }

  const find = (rows: ReturnType<typeof replayRows>, key: string) => {
    const [operator, vehicle] = key.split("|");
    return rows.find((r) => r.operator === operator && r.vehicle === vehicle);
  };

  it("draws a vehicle at its newest fix when the playhead is exactly on it", () => {
    const [a] = consecutive(clean, false);
    for (const smoothing of [false, true]) {
      const row = find(replayRows(w, a.t * 1000, smoothing, NO_LABELS), clean);
      expect(row?.lon).toBe(a.lon);
      expect(row?.lat).toBe(a.lat);
      expect(row?.speed_kmh).toBe(a.spd / 10);
      expect(row?.interpolated).toBe(false);
      expect(row?.ageSeconds).toBe(0);
      expect(row?.event_time).toBe(new Date(a.t * 1000).toISOString());
    }
  });

  it("with smoothing off, keeps the newest real fix between two fixes", () => {
    const [a, b] = consecutive(clean, false);
    const mid = ((a.t + b.t) / 2) * 1000;
    const row = find(replayRows(w, mid, false, NO_LABELS), clean);
    expect(row?.lon).toBe(a.lon);
    expect(row?.lat).toBe(a.lat);
    expect(row?.interpolated).toBe(false);
    expect(row?.ageSeconds).toBe(Math.round((mid / 1000 - a.t)));
  });

  it("with smoothing on, draws a point on the straight line between the two fixes", () => {
    const [a, b] = consecutive(clean, false);
    const quarter = (a.t + (b.t - a.t) / 4) * 1000;
    const row = find(replayRows(w, quarter, true, NO_LABELS), clean);
    expect(row?.lon).toBeCloseTo(a.lon + (b.lon - a.lon) * 0.25, 9);
    expect(row?.lat).toBeCloseTo(a.lat + (b.lat - a.lat) * 0.25, 9);
    expect(row?.interpolated).toBe(true);
    // The real fix behind it is still the earlier one: speed and time are never blended.
    expect(row?.speed_kmh).toBe(a.spd / 10);
    expect(row?.event_time).toBe(new Date(a.t * 1000).toISOString());
  });

  it("never interpolates across a gap of more than 300 s, and stops drawing the vehicle 300 s after its fix", () => {
    const [a, b] = consecutive(gappy, true);
    expect(b.t - a.t).toBeGreaterThan(GAP_SECONDS);

    const within = find(replayRows(w, (a.t + GAP_SECONDS - 1) * 1000, true, NO_LABELS), gappy);
    expect(within?.lon).toBe(a.lon);
    expect(within?.lat).toBe(a.lat);
    expect(within?.interpolated).toBe(false);

    const exactly = find(replayRows(w, (a.t + GAP_SECONDS) * 1000, true, NO_LABELS), gappy);
    expect(exactly?.lon).toBe(a.lon);

    for (const smoothing of [false, true]) {
      const inside = replayRows(w, (a.t + GAP_SECONDS + 1) * 1000, smoothing, NO_LABELS);
      expect(find(inside, gappy)).toBeUndefined();
    }
    // And it is back, at its real fix, the moment the vehicle reports again.
    const back = find(replayRows(w, b.t * 1000, true, NO_LABELS), gappy);
    expect(back?.lon).toBe(b.lon);
    expect(back?.interpolated).toBe(false);
  });

  it("does not draw a vehicle before its first fix", () => {
    const first = oracle.get(clean)?.[0];
    if (!first) throw new Error("no fixes");
    expect(find(replayRows(w, (first.t - 1) * 1000, true, NO_LABELS), clean)).toBeUndefined();
    expect(newestFixAtOrBefore(w, seriesOf(w, clean), first.t - w.originMs / 1000 - 1)).toBe(-1);
  });

  it("draws every vehicle once, with the line of its newest fix and its operator's label", () => {
    const rows = replayRows(w, at("2026-10-12T15:30:00Z"), false, new Map([["rio-brt", "BRT"]]));
    const ids = rows.map((r) => `${r.operator}|${r.vehicle}`);
    expect(new Set(ids).size).toBe(ids.length);
    expect(rows.length).toBeGreaterThan(0);
    for (const r of rows) {
      expect(r.operatorLabel).toBe(r.operator === "rio-brt" ? "BRT" : r.operator);
      expect(r.ageSeconds).toBeLessThanOrEqual(GAP_SECONDS);
      expect(r.heading_deg).toBeNull();
    }
    // A vehicle that served no line stays "no line", not an empty string.
    expect(rows.some((r) => r.line === null)).toBe(true);
  });

  it("reads a missing speed as null and a reported 0 as 0", () => {
    // A copy of real hour 15 where one fix of a clean vehicle has no speed (the recording has none).
    const edited = clone(hour15 as unknown as HourFile);
    const e = edited.vehicles.findIndex((v) => v.vehicle === "7002");
    const idx = edited.fixes.v.findIndex((v) => v === e);
    const zero = edited.fixes.v.findIndex((v, i) => v === e && i > idx);
    const t0 = edited.fixes.t[idx];
    const t1 = edited.fixes.t[zero];
    edited.fixes.spd[idx] = -1;
    edited.fixes.spd[zero] = 0;
    const ew = windowOf([edited]);
    const base = HOUR15_MS / 1000;
    const rowNull = find(replayRows(ew, (base + t0) * 1000, false, NO_LABELS), clean);
    const rowZero = find(replayRows(ew, (base + t1) * 1000, false, NO_LABELS), clean);
    expect(rowNull?.speed_kmh).toBeNull();
    expect(rowZero?.speed_kmh).toBe(0);
  });
});

describe("playhead", () => {
  const w = windowOf(files);

  it("puts minute M at the end of the minute, like the live snapshot", () => {
    const { firstMs, lastMs } = playheadBounds(w);
    expect(firstMs).toBe(HOUR15_MS + MINUTE_MS);
    expect(lastMs).toBe(Date.parse("2026-10-12T16:25:00Z"));
    expect(minuteAt(w, firstMs)?.startMs).toBe(HOUR15_MS);
    expect(minuteAt(w, lastMs)?.startMs).toBe(w.lastMinuteMs);
    // A moment inside a minute belongs to the minute that ends after it.
    expect(minuteAt(w, HOUR15_MS + 5 * MINUTE_MS + 20_000)?.startMs).toBe(HOUR15_MS + 5 * MINUTE_MS);
  });

  it("is null outside the window and clamps into it", () => {
    const { firstMs, lastMs } = playheadBounds(w);
    expect(minuteAt(w, firstMs - 2 * MINUTE_MS)).toBeNull();
    expect(minuteAt(w, lastMs + MINUTE_MS)).toBeNull();
    expect(clampPlayhead(w, firstMs - 1)).toBe(firstMs);
    expect(clampPlayhead(w, lastMs + 1)).toBe(lastMs);
    expect(clampPlayhead(w, firstMs + 5)).toBe(firstMs + 5);
  });
});
