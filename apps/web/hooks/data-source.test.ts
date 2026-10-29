import { describe, expect, it } from "vitest";
import { LIVE_SOURCE, REPLAY_SOURCE, historyUrl, snapshotUrl, selectionHref } from "./data-source";
import { historyHourOptions, historyIndexOptions } from "./use-history";
import { snapshotQueryOptions } from "./use-latest-vehicles";

describe("backend-independent replay sources", () => {
  it("uses only packaged files and a separate cache identity", () => {
    expect(snapshotUrl(REPLAY_SOURCE)).toMatch(/^\/replay-data\/[^/]+\/snapshot.json$/);
    expect(historyUrl(REPLAY_SOURCE)).toMatch(/\/history\/index.json$/);
    expect(historyUrl(REPLAY_SOURCE, "2026-10-12T15")).toMatch(/2026-10-12T15.json$/);
    expect(snapshotQueryOptions(REPLAY_SOURCE).queryKey).not.toEqual(snapshotQueryOptions(LIVE_SOURCE).queryKey);
    expect(historyIndexOptions(REPLAY_SOURCE).queryKey).not.toEqual(historyIndexOptions().queryKey);
    expect(historyHourOptions("2026-10-12T15:00:00Z", REPLAY_SOURCE).queryKey)
      .not.toEqual(historyHourOptions("2026-10-12T15:00:00Z").queryKey);
    expect(snapshotQueryOptions(REPLAY_SOURCE).refetchInterval).toBe(false);
    expect(historyIndexOptions(REPLAY_SOURCE).refetchInterval).toBe(false);
  });

  it("keeps shared minute and line on the recorded map and table routes", () => {
    expect(selectionHref(REPLAY_SOURCE.tableHref, { linha: "1 GAR", t: "20261012T1500Z" }))
      .toBe("/replay/tabela?linha=1+GAR&t=20261012T1500Z");
    expect(selectionHref(REPLAY_SOURCE.mapHref, { linha: null, t: null })).toBe("/replay");
  });
});
