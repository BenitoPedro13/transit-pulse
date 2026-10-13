import { describe, expect, it } from "vitest";
import recorded from "./testdata/line-routes-22.json";
import { feedDate, patternLabel, routeEntry, routeView, type LineRoutes, type RoutesIndex } from "./use-line-routes";

const line = recorded as LineRoutes;

describe("static route selection on the real BRT capture", () => {
  it("retains four patterns and both shapes in each direction with exact stop ordering", () => {
    expect(line.patterns).toHaveLength(4);
    for (const direction of ["0", "1"]) expect(line.patterns.filter((p) => p.direction === direction)).toHaveLength(2);
    for (const pattern of line.patterns) {
      const view = routeView(line, pattern.id)!;
      expect(view.pattern.id).toBe(pattern.id);
      expect(view.shape.id).toBe(pattern.shape);
      expect(view.stops.map((s) => s.id)).toEqual(pattern.stops);
      expect(view.route.agency).toBe("MOBI-Rio");
      expect(patternLabel(line, pattern)).toContain(pattern.headsign);
    }
    expect(routeView(undefined, line.patterns[0].id)).toBeNull();
  });

  it("falls back deterministically if a pattern choice belongs to a different line", () => {
    expect(routeView(line, "previous-line-pattern")).toEqual(routeView(line, null));
    expect(routeView(line, null)!.pattern.trips).toBe(Math.max(...line.patterns.map((p) => p.trips)));
  });

  it("rejects missing referenced stops and invalid coordinates", () => {
    const missing = structuredClone(line);
    missing.stops = [];
    expect(() => routeView(missing, null)).toThrow("stop references");
    const bad = structuredClone(line);
    bad.shapes.forEach((s) => { s.path[0][0] = 181; });
    expect(() => routeView(bad, null)).toThrow("geometry");
  });

  it("matches line names exactly and accepts only local files for the indexed feed", () => {
    const hash = line.feed_hash;
    const index = { feed: { sha256: hash }, lines: [{ line: "22", file: `/gtfs/${hash}/${"a".repeat(64)}.json`, patterns: 4 }] } as RoutesIndex;
    expect(routeEntry(index, "22")).toEqual(index.lines[0]);
    expect(routeEntry(index, "022")).toBeUndefined();
    expect(routeEntry(index, "22x")).toBeUndefined();
    index.lines[0].file = "https://dados.mobilidade.rio/gtfs/schedule";
    expect(() => routeEntry(index, "22")).toThrow("path");
    index.lines[0].file = `/gtfs/${"b".repeat(64)}/${"a".repeat(64)}.json`;
    expect(() => routeEntry(index, "22")).toThrow("path");
  });

  it("formats declared feed dates without treating missing validity as current", () => {
    expect(feedDate("20261231")).toBe("31/12/2026");
    expect(feedDate("")).toBe("não informada");
  });
});
