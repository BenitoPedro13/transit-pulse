import { describe, expect, it } from "vitest";
import snapshotJson from "./testdata/snapshot-20261012.json";
import { formatT, parseParams, parseT, withParams } from "./dashboard-url";
import { fleetStats, formatKmh, formatPercent, median } from "./use-fleet-stats";
import { toRows, type Snapshot, type Vehicle, type VehicleRow } from "./use-latest-vehicles";
import { compareLines, matchLines, toLineIndex } from "./use-lines";
import { findVehicle, type SelectedVehicle } from "./use-selected-vehicle";
import { compassPoint, formatHeading } from "./heading";
import { letreiroText } from "./letreiro-text";
import { speedBin } from "./speed-bins";

// A real response of apps/api (the recorded run's last minute, 2026-10-12T16:24Z), cut to 254
// vehicles on 19 lines plus 40 with no line: BRT, null headings and null lines kept. Expected values
// below were computed with jq from this same file, independently of the code under test.
const snapshot = snapshotJson as Snapshot;
const vehicles = snapshot.vehicles;
const rows = toRows(snapshot);

describe("fleetStats on the real snapshot", () => {
  const stats = fleetStats(vehicles);

  it("counts by mode", () => {
    expect(stats).toMatchObject({ total: 254, bus: 137, brt: 117, other: 0 });
  });

  it("counts the moving share among vehicles that report a speed", () => {
    expect(stats).toMatchObject({ withSpeed: 254, moving: 80, noSpeed: 0 });
    expect(stats.movingShare).toBeCloseTo(80 / 254, 10);
    expect(formatPercent(stats.movingShare ?? 0)).toBe("31%");
  });

  it("takes the median speed of the moving vehicles only", () => {
    expect(stats.medianMovingKmh).toBe(36.95);
    expect(formatKmh(stats.medianMovingKmh ?? 0)).toBe("37,0 km/h");
  });

  it("lists the 8 busiest lines, ties in natural order, and never counts line: null as a line", () => {
    expect(stats.topLines).toEqual([
      { line: "10", count: 20 },
      { line: "483", count: 20 },
      { line: "639", count: 20 },
      { line: "ESPECIAL", count: 20 },
      { line: "GARAGEM", count: 20 },
      { line: "0", count: 19 },
      { line: "17", count: 19 },
      { line: "46", count: 17 },
    ]);
  });
});

describe("fleetStats with a missing speed", () => {
  // The recorded run has no null speed (0 of 1,099,188 rows), but the contract allows one. These rows
  // are real rows with the speed removed, to test the branch the data never exercised.
  const some: Vehicle[] = vehicles.slice(0, 10).map((v, i) => ({ ...v, speed_kmh: i < 4 ? null : v.speed_kmh }));
  const stats = fleetStats(some);

  it("keeps a missing speed apart: not stopped, not moving, not in the share", () => {
    expect(stats.noSpeed).toBe(4);
    expect(stats.withSpeed).toBe(6);
    expect(stats.withSpeed + stats.noSpeed).toBe(stats.total);
    expect(stats.moving + (stats.withSpeed - stats.moving) + stats.noSpeed).toBe(stats.total);
    expect(stats.movingShare).toBeCloseTo(stats.moving / 6, 10);
  });

  it("has no share and no median when nothing reports a speed or moves", () => {
    const none = fleetStats(some.map((v) => ({ ...v, speed_kmh: null })));
    expect(none).toMatchObject({ withSpeed: 0, moving: 0, noSpeed: 10, movingShare: null, medianMovingKmh: null });
    const stopped = fleetStats(some.map((v) => ({ ...v, speed_kmh: 0 })));
    expect(stopped).toMatchObject({ withSpeed: 10, moving: 0, movingShare: 0, medianMovingKmh: null });
  });

  it("counts a reported 0 as stopped and 3 as moving", () => {
    const edge = fleetStats([
      { mode: "bus", line: "1", speed_kmh: 0 },
      { mode: "bus", line: "1", speed_kmh: 2.9 },
      { mode: "bus", line: "1", speed_kmh: 3 },
    ]);
    expect(edge).toMatchObject({ withSpeed: 3, moving: 1, medianMovingKmh: 3 });
  });

  it("puts an unknown mode in other, so the total still adds up", () => {
    const odd = fleetStats([{ mode: "ferry", line: null, speed_kmh: 10 }]);
    expect(odd).toMatchObject({ total: 1, bus: 0, brt: 0, other: 1 });
  });
});

describe("median", () => {
  it("handles odd, even and empty", () => {
    expect(median([1, 2, 9])).toBe(2);
    expect(median([1, 2, 3, 10])).toBe(2.5);
    expect(median([])).toBeNull();
  });
});

describe("compareLines", () => {
  it("is a natural order: numbers before words, digits as numbers", () => {
    expect(["ESPECIAL", "343", "10", "2"].sort(compareLines)).toEqual(["2", "10", "343", "ESPECIAL"]);
    expect(["b2", "b10", "B1", "a"].sort(compareLines)).toEqual(["a", "B1", "b2", "b10"]);
  });

  it("is stable for lines that compare equal as numbers", () => {
    expect(["7", "007"].sort(compareLines)).toEqual(["007", "7"]);
    expect(["007", "7"].sort(compareLines)).toEqual(["007", "7"]);
  });
});

describe("toLineIndex", () => {
  const index = toLineIndex(vehicles);

  it("lists every real line once, in natural order, with its count", () => {
    expect(index.lines.map((l) => l.line)).toEqual([
      "0", "1 GAR", "007", "10", "12", "17", "22", "25", "35", "46", "51", "53", "100", "483", "639", "2111",
      "ESP01", "ESPECIAL", "GARAGEM",
    ]);
    expect(index.lines.find((l) => l.line === "483")).toEqual({ line: "483", mode: "bus", count: 20 });
    expect(index.lines.find((l) => l.line === "ESP01")).toEqual({ line: "ESP01", mode: "brt", count: 4 });
  });

  it("keeps vehicles with no line as a count, never as a line", () => {
    expect(index.unidentified).toBe(40);
    expect(index.lines.reduce((n, l) => n + l.count, 0) + index.unidentified).toBe(vehicles.length);
  });

  it("holds the assumption that one line is one mode", () => {
    const modes = new Map<string, Set<string>>();
    for (const v of vehicles) if (v.line !== null) modes.set(v.line, (modes.get(v.line) ?? new Set()).add(v.mode));
    for (const [line, set] of modes) expect(set.size, `line ${line}`).toBe(1);
  });
});

describe("matchLines", () => {
  const { lines } = toLineIndex(vehicles);

  it("matches the start of the line, ignoring case and spaces around the query", () => {
    expect(matchLines(lines, "1").map((l) => l.line)).toEqual(["1 GAR", "10", "12", "17", "100"]);
    expect(matchLines(lines, " esp ").map((l) => l.line)).toEqual(["ESP01", "ESPECIAL"]);
    expect(matchLines(lines, "47")).toEqual([]);
  });

  it("keeps everything for an empty query", () => {
    expect(matchLines(lines, "")).toBe(lines);
    expect(matchLines(lines, "   ")).toBe(lines);
  });
});

describe("speedBin", () => {
  it("uses half-open bins: <3, 3-15, 15-30, 30-50, >=50", () => {
    expect([0, 2.99, 3, 14.9, 15, 29.9, 30, 49.9, 50, 120].map(speedBin)).toEqual([0, 0, 1, 1, 2, 2, 3, 3, 4, 4]);
  });

  it("gives no bin to a missing speed", () => {
    expect(speedBin(null)).toBeNull();
    expect(speedBin(undefined)).toBeNull();
    expect(speedBin(Number.NaN)).toBeNull();
  });

  it("puts the real snapshot's speeds in the counts jq gives", () => {
    const counts = [0, 0, 0, 0, 0];
    for (const v of vehicles) counts[speedBin(v.speed_kmh) ?? 0]++;
    expect(counts).toEqual([174, 12, 18, 26, 24]);
  });
});

describe("dashboard URL", () => {
  it("reads the three parameters", () => {
    expect(parseParams(new URLSearchParams("linha=474&onibus=A29001&cor=velocidade"))).toEqual({
      linha: "474",
      onibus: "A29001",
      cor: "velocidade",
      t: null,
    });
  });

  it("falls back to the default view for missing, empty or unknown values", () => {
    expect(parseParams(new URLSearchParams(""))).toEqual({ linha: null, onibus: null, cor: "modo", t: null });
    expect(parseParams(new URLSearchParams("linha=&onibus=%20&cor=rosa"))).toEqual({ linha: null, onibus: null, cor: "modo", t: null });
  });

  it("writes a patch and leaves out defaults and other keys' order", () => {
    expect(withParams("", { linha: "474" })).toBe("linha=474");
    expect(withParams("linha=474&cor=velocidade", { onibus: "A1" })).toBe("linha=474&cor=velocidade&onibus=A1");
    expect(withParams("linha=474&onibus=A1", { linha: null, onibus: null })).toBe("");
    expect(withParams("cor=velocidade", { cor: "modo" })).toBe("");
    expect(withParams("", { cor: "velocidade" })).toBe("cor=velocidade");
  });

  it("reads a replay minute only when it is a whole minute in UTC", () => {
    expect(parseParams(new URLSearchParams("t=2026-10-12T12:47Z")).t).toBe("2026-10-12T12:47Z");
    expect(parseT("2026-10-12T12:47Z")).toBe(Date.parse("2026-10-12T12:47:00Z"));
    for (const bad of ["", "2026-10-12", "2026-10-12T12:47", "2026-10-12T12:47:30Z", "2026-10-12T12:47+03:00", "2026-13-40T99:99Z", "agora"]) {
      expect(parseT(bad), bad).toBeNull();
      expect(parseParams(new URLSearchParams({ t: bad })).t, bad).toBeNull();
    }
    expect(parseT(null)).toBeNull();
  });

  it("writes the minute back to the same text, and drops it to go live", () => {
    expect(formatT(Date.parse("2026-10-12T12:47:00Z"))).toBe("2026-10-12T12:47Z");
    expect(formatT(parseT("2026-10-12T00:00Z") ?? 0)).toBe("2026-10-12T00:00Z");
    expect(withParams("linha=474", { t: "2026-10-12T12:47Z" })).toBe("linha=474&t=2026-10-12T12%3A47Z");
    expect(withParams("linha=474&t=2026-10-12T12%3A47Z", { t: null })).toBe("linha=474");
  });

  it("encodes a line with a space", () => {
    expect(withParams("", { linha: "1 GAR" })).toBe("linha=1+GAR");
    expect(parseParams(new URLSearchParams("linha=1+GAR")).linha).toBe("1 GAR");
  });
});

describe("findVehicle", () => {
  it("finds a bus in the snapshot and answers undefined for one that isn't there", () => {
    expect(findVehicle(rows, "B31001")).toMatchObject({ vehicle: "B31001", line: "483" });
    expect(findVehicle(rows, "ZZZ999")).toBeUndefined();
  });
});

describe("compass", () => {
  it("names the eight points, centred on each", () => {
    expect([0, 45, 90, 135, 180, 225, 270, 315].map(compassPoint)).toEqual([
      "norte", "nordeste", "leste", "sudeste", "sul", "sudoeste", "oeste", "noroeste",
    ]);
    expect([359, 22, 23, 337, 338, -45, 405].map(compassPoint)).toEqual([
      "norte", "norte", "nordeste", "noroeste", "norte", "noroeste", "nordeste",
    ]);
  });

  it("says 'sem direção' for a missing heading, never north", () => {
    expect(formatHeading(135)).toBe("135°, sudeste");
    expect(formatHeading(0)).toBe("0°, norte");
    expect(formatHeading(null)).toBe("sem direção");
  });

  it("holds for every heading in the real snapshot, nulls included", () => {
    const nulls = vehicles.filter((v) => v.heading_deg === null).length;
    expect(nulls).toBe(53);
    expect(vehicles.filter((v) => formatHeading(v.heading_deg) === "sem direção")).toHaveLength(nulls);
  });
});

describe("letreiroText", () => {
  const base = {
    status: "live" as const,
    asOfMinute: "2026-10-12T16:24:00Z",
    total: 2627,
    linha: null,
    lineCount: 0,
    selected: { kind: "none" } as SelectedVehicle,
    onibus: null,
    timeZone: "America/Sao_Paulo",
  };
  const found = (row: VehicleRow): SelectedVehicle => ({ kind: "found", row });

  it("reads the clock and the count by default", () => {
    expect(letreiroText(base)).toBe("13:24 · 2.627 ÔNIBUS NA RUA");
  });

  it("reads the line and its count when a line is selected", () => {
    expect(letreiroText({ ...base, linha: "483", lineCount: 20 })).toBe("483 · 20 ÔNIBUS");
    expect(letreiroText({ ...base, linha: "ESP01", lineCount: 0 })).toBe("ESP01 · 0 ÔNIBUS");
  });

  it("reads the line and the vehicle when a bus is selected, and it wins over the line", () => {
    const row = findVehicle(rows, "B31001")!;
    expect(letreiroText({ ...base, linha: "10", lineCount: 21, onibus: "B31001", selected: found(row) })).toBe("483 · B31001");
    const nolineRow = rows.find((r) => r.line === null)!;
    expect(letreiroText({ ...base, onibus: nolineRow.vehicle, selected: found(nolineRow) })).toBe(`SEM LINHA · ${nolineRow.vehicle}`);
  });

  it("says a bus isn't there instead of showing a position", () => {
    const text = letreiroText({ ...base, onibus: "zz9", selected: { kind: "missing", asOfMinute: base.asOfMinute } });
    expect(text).toBe("ZZ9 · NÃO APARECE");
  });

  it("starts with ATRASADO or SEM SINAL when the data isn't live", () => {
    expect(letreiroText({ ...base, status: "stale" })).toBe("ATRASADO · 13:24 · 2.627 ÔNIBUS NA RUA");
    expect(letreiroText({ ...base, status: "failed", linha: "10", lineCount: 21 })).toBe("SEM SINAL · 10 · 21 ÔNIBUS");
  });

  it("covers the states before there is a snapshot", () => {
    expect(letreiroText({ ...base, status: "loading", asOfMinute: null })).toBe("CARREGANDO");
    expect(letreiroText({ ...base, status: "unavailable", asOfMinute: null })).toBe("SEM SINAL");
  });
});
