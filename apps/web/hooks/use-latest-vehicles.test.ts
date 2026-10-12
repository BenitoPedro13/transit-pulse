import { describe, expect, it } from "vitest";
import {
  filterByLine,
  formatAge,
  formatCoverage,
  formatNullable,
  formatTotal,
  statusOf,
  toRows,
  type Coverage,
  type Snapshot,
} from "./use-latest-vehicles";

// Shaped like a real response of apps/api (the recorded run's last minute), trimmed.
const AS_OF = "2026-10-12T16:24:00Z";
const AS_OF_MS = Date.parse(AS_OF);

function coverage(operator: string, label: string, by: Record<string, number>): Coverage {
  const total = Object.values(by).reduce((a, b) => a + b, 0);
  return { operator, label, mode: "bus", tier: "live", minute_utc: AS_OF, feeds_total: total, feeds_by_status: by };
}

function snapshot(cov: Coverage[]): Snapshot {
  return {
    as_of_minute: AS_OF,
    generated_at: "2026-10-12T16:25:31Z",
    window_seconds: 300,
    coverage: cov,
    vehicles: [
      { operator: "rio-sppo", mode: "bus", vehicle: "A48011", line: "457", lat: -22.9, lon: -43.2, event_time: "2026-10-12T16:24:41Z", speed_kmh: 0, heading_deg: null },
      { operator: "rio-sppo", mode: "bus", vehicle: "A48064", line: null, lat: -22.9, lon: -43.2, event_time: "2026-10-12T16:20:30Z", speed_kmh: null, heading_deg: 5 },
      { operator: "rio-brt", mode: "brt", vehicle: "7000", line: "ESP01", lat: -22.9, lon: -43.2, event_time: "2026-10-12T16:24:59Z", speed_kmh: 31.5, heading_deg: 307 },
    ],
  };
}

const ok = [coverage("rio-sppo", "Ônibus municipais (SPPO)", { ok: 2 }), coverage("rio-brt", "BRT", { ok: 1 })];

describe("formatNullable", () => {
  it("shows null as a dash, never as 0", () => {
    expect(formatNullable(null)).toBe("—");
    expect(formatNullable(undefined)).toBe("—");
    expect(formatNullable(null, "km/h")).toBe("—");
  });
  it("keeps a reported 0", () => {
    expect(formatNullable(0)).toBe("0");
    expect(formatNullable(0, "km/h")).toBe("0 km/h");
  });
  it("formats fractions the pt-BR way and passes text through", () => {
    expect(formatNullable(31.5, "km/h")).toBe("31,5 km/h");
    expect(formatNullable("457")).toBe("457");
    expect(formatNullable(307, "°")).toBe("307°");
  });
});

describe("formatAge", () => {
  // The reference is the end of the as_of minute, 16:25:00.
  it("counts from the end of the snapshot's minute", () => {
    expect(formatAge("2026-10-12T16:24:59Z", AS_OF)).toBe("1 s");
    expect(formatAge("2026-10-12T16:24:41Z", AS_OF)).toBe("19 s");
    expect(formatAge("2026-10-12T16:20:30Z", AS_OF)).toBe("4 min 30 s");
    expect(formatAge("2026-10-12T16:20:00Z", AS_OF)).toBe("5 min 0 s");
  });
  it("does not invent an age for a fix after that minute or for bad input", () => {
    expect(formatAge("2026-10-12T16:25:30Z", AS_OF)).toBe("—");
    expect(formatAge("not a date", AS_OF)).toBe("—");
  });
});

describe("statusOf", () => {
  const tz = "UTC";
  it("is live when the newest minute is at most 5 minutes old", () => {
    const s = statusOf(snapshot(ok), AS_OF_MS + 90_000, tz);
    expect(s.kind).toBe("live");
    expect(s.message).toBe("Ao vivo · atualizado 16:24");
    expect(statusOf(snapshot(ok), AS_OF_MS + 5 * 60_000, tz).kind).toBe("live");
  });
  it("is stale after 5 minutes, naming the minute in the given time zone", () => {
    const s = statusOf(snapshot(ok), AS_OF_MS + 5 * 60_000 + 1, tz);
    expect(s.kind).toBe("stale");
    expect(s.message).toBe("Não está ao vivo: mostrando o último minuto gravado, 16:24");
    expect(statusOf(snapshot(ok), AS_OF_MS + 3600_000, "America/Sao_Paulo").message).toContain("13:24");
  });
  it("is failed when an operator's feeds all failed, even if the snapshot is fresh", () => {
    const cov = [coverage("rio-sppo", "Ônibus municipais (SPPO)", { failed: 2 }), coverage("rio-brt", "BRT", { ok: 1 })];
    const s = statusOf(snapshot(cov), AS_OF_MS + 60_000, tz);
    expect(s.kind).toBe("failed");
    expect(s.message).toContain("Ônibus municipais (SPPO)");
    expect(s.message).not.toContain("BRT");
  });
  it("treats a gap like a failure, and one working feed as arriving", () => {
    expect(statusOf(snapshot([coverage("rio-sppo", "SPPO", { gap: 2 })]), AS_OF_MS, tz).kind).toBe("failed");
    expect(statusOf(snapshot([coverage("rio-sppo", "SPPO", { ok: 1, failed: 1 })]), AS_OF_MS, tz).kind).toBe("live");
    expect(statusOf(snapshot([coverage("rio-sppo", "SPPO", { provider_not_ok: 2 })]), AS_OF_MS, tz).kind).toBe("live");
  });
});

describe("toRows and filterByLine", () => {
  const rows = toRows(snapshot(ok));
  it("adds the operator label and the age, and keeps nulls", () => {
    expect(rows[0]).toMatchObject({ operatorLabel: "Ônibus municipais (SPPO)", ageSeconds: 19, speed_kmh: 0, heading_deg: null });
    expect(rows[1]).toMatchObject({ line: null, speed_kmh: null, ageSeconds: 270 });
    expect(rows[2].operatorLabel).toBe("BRT");
  });
  it("falls back to the operator id, not a made-up label", () => {
    const s = snapshot([coverage("rio-brt", "BRT", { ok: 1 })]);
    expect(toRows(s)[0].operatorLabel).toBe("rio-sppo");
  });
  it("filters by line prefix, ignoring case, and drops vehicles with no line", () => {
    expect(filterByLine(rows, "45").map((r) => r.vehicle)).toEqual(["A48011"]);
    expect(filterByLine(rows, " esp").map((r) => r.vehicle)).toEqual(["7000"]);
    expect(filterByLine(rows, "9")).toEqual([]);
    expect(filterByLine(rows, "")).toBe(rows);
    expect(filterByLine(rows, "   ")).toBe(rows);
  });
});

describe("text", () => {
  it("formats the total with pt-BR separators and plurals", () => {
    expect(formatTotal(10412, 3)).toBe("10.412 veículos de 3 operadores");
    expect(formatTotal(1, 1)).toBe("1 veículo de 1 operador");
  });
  it("describes coverage per status, never naming a feed", () => {
    expect(formatCoverage(coverage("rio-sppo", "SPPO", { ok: 2 }))).toBe("SPPO: com posições em 2 de 2 fontes");
    expect(formatCoverage(coverage("rio-brt", "BRT", { ok: 1 }))).toBe("BRT: com posições em 1 de 1 fonte");
    expect(formatCoverage(coverage("rio-sppo", "SPPO", { ok: 1, failed: 1 }))).toBe(
      "SPPO: com posições em 1 de 2 fontes, falha ao buscar em 1 de 2 fontes",
    );
    expect(formatCoverage({ ...coverage("x", "X", { ok: 1 }), tier: "scheduled" })).toContain("(scheduled)");
  });
});
