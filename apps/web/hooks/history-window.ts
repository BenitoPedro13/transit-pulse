import type { VehicleRow } from "./use-latest-vehicles";

// The pure rules of the time machine (docs/tasks/TASK-time-machine.md §2.1, §2.4). They take the
// history-hour.v1 files exactly as the API serves them and answer three questions: what does the
// window contain, where is everybody at a given moment, and which minutes are gaps. No React, no deck.gl.

// Two consecutive fixes of a vehicle more than this far apart are two separate paths: the gap is never
// bridged (invariant 1). It is also how old a fix may be and still be drawn (the snapshot's own rule).
export const GAP_SECONDS = 300;
export const MINUTE_MS = 60_000;

export type HourFeeds = { ok: number; failed: number; provider_not_ok: number; gap: number; missing: number };

// history-hour.v1 (packages/contracts/history-hour.v1.schema.json).
export type HourFile = {
  schema: "history-hour.v1";
  hour_utc: string;
  complete: boolean;
  sampling: "newest_fix_per_vehicle_per_minute";
  vehicles: { operator: string; vehicle: string; mode: string; line: string | null }[];
  fixes: {
    v: number[];
    t: number[];
    // Degrees times 1e5.
    lat: number[];
    lon: number[];
    // km/h times 10; -1 means the provider sent none.
    spd: number[];
  };
  minutes: { vehicles: number; feeds: HourFeeds }[];
};

// GET /v1/history
export type HistoryIndex = {
  newest_minute: string;
  oldest_minute: string;
  hours: string[];
  window_hours: number;
};

// One minute of the window, for the chart and the scrubber's text.
export type MinuteSample = {
  startMs: number;
  // Vehicles with a fix in this minute. Meaningless when `gap` is true (nothing was reported).
  vehicles: number;
  feeds: HourFeeds;
  // No feed reported anything: every feed failed, has a gap, or was never recorded. Drawn as a hole,
  // never as 0 vehicles (invariant 6). A minute where only some feeds failed is not a gap.
  gap: boolean;
  feedsTotal: number;
};

export function isGapMinute(feeds: HourFeeds): boolean {
  return feeds.ok + feeds.provider_not_ok === 0;
}

function feedsTotal(feeds: HourFeeds): number {
  return feeds.ok + feeds.failed + feeds.provider_not_ok + feeds.gap + feeds.missing;
}

// Every recorded fix of the window as parallel arrays, grouped by vehicle ("series") and ordered by time
// inside a series. A series is one operator + vehicle, whatever line it served.
export type HistoryWindow = {
  // Epoch ms of t = 0: the start of the first hour loaded.
  originMs: number;
  // The first and last minute that any feed has a row for. The lead-in before the recording started and
  // the minutes after the newest one are not part of the window.
  firstMinuteMs: number;
  lastMinuteMs: number;
  minutes: MinuteSample[];
  lines: (string | null)[];
  series: { operator: string; vehicle: string; mode: string }[];
  // Series s owns fixes [starts[s], starts[s + 1]).
  starts: Uint32Array;
  // Seconds since originMs. Whole seconds, so Float32 is exact for hours.
  t: Float32Array;
  // lon, lat interleaved, in degrees.
  xy: Float64Array;
  // km/h times 10; -1 is "no speed".
  spd: Int16Array;
  line: Uint16Array;
  // Paths: runs of one series' fixes with no gap over GAP_SECONDS. Path p owns fixes
  // [pathStarts[p], pathStarts[p + 1]) (the last path ends at t.length) and belongs to series pathSeries[p].
  pathStarts: Uint32Array;
  pathSeries: Uint32Array;
  pathLine: Uint16Array;
};

export class HistoryFormatError extends Error {}

// Builds the window from hour files in any order. An hour that is missing leaves its minutes as gaps.
// Returns null when no minute of any file has a feed row (nothing recorded).
export function buildWindow(files: HourFile[]): HistoryWindow | null {
  if (files.length === 0) return null;
  const hours = [...files].sort((a, b) => Date.parse(a.hour_utc) - Date.parse(b.hour_utc));
  for (const f of hours) checkFile(f);

  const originMs = Date.parse(hours[0].hour_utc);
  const endMs = Date.parse(hours[hours.length - 1].hour_utc) + 60 * MINUTE_MS;

  // Every minute from the first hour to the end of the last, hours that are absent included.
  const all: MinuteSample[] = [];
  for (let ms = originMs; ms < endMs; ms += MINUTE_MS) {
    all.push({ startMs: ms, vehicles: 0, feeds: { ok: 0, failed: 0, provider_not_ok: 0, gap: 0, missing: 0 }, gap: true, feedsTotal: 0 });
  }
  for (const f of hours) {
    const first = (Date.parse(f.hour_utc) - originMs) / MINUTE_MS;
    f.minutes.forEach((m, i) => {
      const total = feedsTotal(m.feeds);
      all[first + i] = { startMs: originMs + (first + i) * MINUTE_MS, vehicles: m.vehicles, feeds: m.feeds, gap: isGapMinute(m.feeds), feedsTotal: total };
    });
  }
  // A minute every feed has no row for, at either end, is outside the recording.
  const recorded = (m: MinuteSample) => m.feeds.missing < m.feedsTotal;
  const firstIdx = all.findIndex(recorded);
  if (firstIdx < 0) return null;
  let lastIdx = all.length - 1;
  while (!recorded(all[lastIdx])) lastIdx--;
  const minutes = all.slice(firstIdx, lastIdx + 1);

  // Series and the line table, over every hour.
  const seriesIndex = new Map<string, number>();
  const series: HistoryWindow["series"] = [];
  const lines: (string | null)[] = [];
  const lineIndex = new Map<string | null, number>();
  const entrySeries: Int32Array[] = [];
  const entryLine: Int32Array[] = [];
  let total = 0;
  for (const f of hours) {
    const toSeries = new Int32Array(f.vehicles.length);
    const toLine = new Int32Array(f.vehicles.length);
    f.vehicles.forEach((e, i) => {
      const key = `${e.operator}\u0000${e.vehicle}`;
      let s = seriesIndex.get(key);
      if (s === undefined) {
        s = series.length;
        seriesIndex.set(key, s);
        series.push({ operator: e.operator, vehicle: e.vehicle, mode: e.mode });
      }
      let l = lineIndex.get(e.line);
      if (l === undefined) {
        l = lines.length;
        lineIndex.set(e.line, l);
        lines.push(e.line);
      }
      toSeries[i] = s;
      toLine[i] = l;
    });
    entrySeries.push(toSeries);
    entryLine.push(toLine);
    total += f.fixes.v.length;
  }

  // Count, then fill, so every series' fixes end up contiguous.
  const starts = new Uint32Array(series.length + 1);
  hours.forEach((f, h) => {
    for (const e of f.fixes.v) starts[entrySeries[h][e] + 1]++;
  });
  for (let s = 0; s < series.length; s++) starts[s + 1] += starts[s];
  const cursor = starts.slice(0, series.length);
  const t = new Float32Array(total);
  const xy = new Float64Array(total * 2);
  const spd = new Int16Array(total);
  const line = new Uint16Array(total);
  hours.forEach((f, h) => {
    const offset = (Date.parse(f.hour_utc) - originMs) / 1000;
    const { v, t: ft, lat, lon, spd: fs } = f.fixes;
    for (let i = 0; i < v.length; i++) {
      const k = cursor[entrySeries[h][v[i]]]++;
      t[k] = offset + ft[i];
      xy[2 * k] = lon[i] / 1e5;
      xy[2 * k + 1] = lat[i] / 1e5;
      spd[k] = fs[i];
      line[k] = entryLine[h][v[i]];
    }
  });

  // A vehicle that changed line inside an hour has two entries, so its fixes can arrive out of order.
  for (let s = 0; s < series.length; s++) sortSeries(starts[s], starts[s + 1], t, xy, spd, line);

  // Paths.
  const pathStartList: number[] = [];
  const pathSeriesList: number[] = [];
  for (let s = 0; s < series.length; s++) {
    for (let k = starts[s]; k < starts[s + 1]; k++) {
      if (k === starts[s] || t[k] - t[k - 1] > GAP_SECONDS) {
        pathStartList.push(k);
        pathSeriesList.push(s);
      }
    }
  }
  const pathStarts = Uint32Array.from(pathStartList);
  const pathSeries = Uint32Array.from(pathSeriesList);
  // A path's line is the line of its first fix.
  const pathLine = Uint16Array.from(pathStartList, (k) => line[k]);

  return {
    originMs,
    firstMinuteMs: minutes[0].startMs,
    lastMinuteMs: minutes[minutes.length - 1].startMs,
    minutes,
    lines,
    series,
    starts,
    t,
    xy,
    spd,
    line,
    pathStarts,
    pathSeries,
    pathLine,
  };
}

function checkFile(f: HourFile) {
  if (f.schema !== "history-hour.v1") throw new HistoryFormatError(`unknown schema ${String(f.schema)}`);
  if (f.minutes.length !== 60) throw new HistoryFormatError(`${f.hour_utc}: ${f.minutes.length} minutes, expected 60`);
  const n = f.fixes.v.length;
  if (f.fixes.t.length !== n || f.fixes.lat.length !== n || f.fixes.lon.length !== n || f.fixes.spd.length !== n) {
    throw new HistoryFormatError(`${f.hour_utc}: fix columns differ in length`);
  }
}

// Sorts one series' range by time when it is not already (rare, so the common case is one pass).
function sortSeries(from: number, to: number, t: Float32Array, xy: Float64Array, spd: Int16Array, line: Uint16Array) {
  let sorted = true;
  for (let k = from + 1; k < to; k++) {
    if (t[k] < t[k - 1]) {
      sorted = false;
      break;
    }
  }
  if (sorted) return;
  const order = Array.from({ length: to - from }, (_, i) => from + i).sort((a, b) => t[a] - t[b]);
  const t2 = order.map((k) => t[k]);
  const x2 = order.map((k) => xy[2 * k]);
  const y2 = order.map((k) => xy[2 * k + 1]);
  const s2 = order.map((k) => spd[k]);
  const l2 = order.map((k) => line[k]);
  order.forEach((_, i) => {
    const k = from + i;
    t[k] = t2[i];
    xy[2 * k] = x2[i];
    xy[2 * k + 1] = y2[i];
    spd[k] = s2[i];
    line[k] = l2[i];
  });
}

// The playhead is a moment, and a minute is "as of its end", like the live snapshot (its fixes are aged
// to the end of as_of_minute). So minute M sits at M + 60 s, and the minute shown for a moment is the one
// that ends at or after it.
export type PlayheadBounds = { firstMs: number; lastMs: number };

export function playheadBounds(w: HistoryWindow): PlayheadBounds {
  return { firstMs: w.firstMinuteMs + MINUTE_MS, lastMs: w.lastMinuteMs + MINUTE_MS };
}

export function minuteStart(playheadMs: number): number {
  return Math.floor((playheadMs - 1) / MINUTE_MS) * MINUTE_MS;
}

export function clampPlayhead(w: HistoryWindow, playheadMs: number): number {
  const { firstMs, lastMs } = playheadBounds(w);
  return Math.min(lastMs, Math.max(firstMs, playheadMs));
}

// The minute the playhead is in, or null outside the window.
export function minuteAt(w: HistoryWindow, playheadMs: number): MinuteSample | null {
  const i = (minuteStart(playheadMs) - w.firstMinuteMs) / MINUTE_MS;
  return Number.isInteger(i) ? (w.minutes[i] ?? null) : null;
}

// Index of the newest fix of series s at or before tSec (seconds since originMs), or -1.
export function newestFixAtOrBefore(w: HistoryWindow, s: number, tSec: number): number {
  let lo = w.starts[s];
  let hi = w.starts[s + 1] - 1;
  let found = -1;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    if (w.t[mid] <= tSec) {
      found = mid;
      lo = mid + 1;
    } else {
      hi = mid - 1;
    }
  }
  return found;
}

// A vehicle as the replay draws it: the same fields as a live row, so the map layer, the card and the
// panels take either. `interpolated` says the position is smoothed between two real fixes; event_time is
// then the real fix just before the playhead.
export type ReplayRow = VehicleRow & { interpolated: boolean };

// Where everybody is at the playhead.
//   smoothing off: each vehicle at its newest fix at or before the playhead, if that fix is at most
//                  GAP_SECONDS old (the snapshot's rule). Positions jump once a minute.
//   smoothing on:  the same, except a vehicle between two fixes of one path is drawn on the straight line
//                  between them. Nothing is extrapolated past the last fix, and nothing is drawn across a gap.
// operatorLabels maps an operator id to its name; an operator not in it shows its id.
export function replayRows(w: HistoryWindow, playheadMs: number, smoothing: boolean, operatorLabels: ReadonlyMap<string, string>): ReplayRow[] {
  const tSec = (playheadMs - w.originMs) / 1000;
  const rows: ReplayRow[] = [];
  for (let s = 0; s < w.series.length; s++) {
    const k = newestFixAtOrBefore(w, s, tSec);
    if (k < 0) continue;
    const age = tSec - w.t[k];
    if (age > GAP_SECONDS) continue;

    let lon = w.xy[2 * k];
    let lat = w.xy[2 * k + 1];
    let interpolated = false;
    if (smoothing && age > 0 && k + 1 < w.starts[s + 1]) {
      const span = w.t[k + 1] - w.t[k];
      if (span <= GAP_SECONDS) {
        const f = age / span;
        lon += (w.xy[2 * k + 2] - lon) * f;
        lat += (w.xy[2 * k + 3] - lat) * f;
        interpolated = true;
      }
    }

    const info = w.series[s];
    const speed = w.spd[k];
    rows.push({
      operator: info.operator,
      mode: info.mode,
      vehicle: info.vehicle,
      line: w.lines[w.line[k]],
      lat,
      lon,
      event_time: new Date(w.originMs + w.t[k] * 1000).toISOString(),
      speed_kmh: speed < 0 ? null : speed / 10,
      heading_deg: null,
      operatorLabel: operatorLabels.get(info.operator) ?? info.operator,
      ageSeconds: Math.round(age),
      interpolated,
    });
  }
  return rows;
}

// The data TripsLayer draws: one path per run of fixes, as binary attributes (no per-vertex objects).
export type TripsData = {
  length: number;
  startIndices: Uint32Array;
  attributes: {
    getPath: { value: Float64Array; size: 2 };
    getTimestamps: { value: Float32Array; size: 1 };
  };
};

// One object per window, so deck.gl sees the same `data` on every frame and tessellates the paths once.
const trips = new WeakMap<HistoryWindow, TripsData>();

export function tripsData(w: HistoryWindow): TripsData {
  let data = trips.get(w);
  if (!data) {
    data = {
      length: w.pathSeries.length,
      startIndices: w.pathStarts,
      attributes: { getPath: { value: w.xy, size: 2 }, getTimestamps: { value: w.t, size: 1 } },
    };
    trips.set(w, data);
  }
  return data;
}

// The scrubber's text for the playhead's minute: "12:47, 2.598 ônibus", or "12:47, falha na coleta".
// A minute some feeds missed says how many reported.
export function minuteText(m: MinuteSample, clock: string, count: (n: number) => string): string {
  if (m.gap) return `${clock}, falha na coleta`;
  const partial = m.feeds.ok < m.feedsTotal ? `, ${m.feeds.ok} de ${m.feedsTotal} fontes` : "";
  return `${clock}, ${count(m.vehicles)} ônibus${partial}`;
}
