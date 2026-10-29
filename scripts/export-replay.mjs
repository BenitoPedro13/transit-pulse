#!/usr/bin/env node
// Freeze a verified real history API projection. To export older dates, rebuild from raw into
// an isolated database first. Never fills missing minutes or changes the sampling contract.
import { createHash } from "node:crypto";
import { mkdir, readFile, rename, stat, writeFile } from "node:fs/promises";
import { basename, resolve } from "node:path";
import { parseArgs } from "node:util";

const { values } = parseArgs({ options: {
  source: { type: "string" }, out: { type: "string" }, "raw-manifest": { type: "string" },
} });
if (!values.source || !values.out || !values["raw-manifest"]) {
  throw new Error("Usage: node scripts/export-replay.mjs --source http://127.0.0.1:8096/v1 --out apps/web/public/replay-data/2026-10-12 --raw-manifest var/runs/20261012-its-adapter-raw.sha256");
}
const source = new URL(values.source);
if (!["http:", "https:"].includes(source.protocol) || source.username || source.password || source.search || source.hash) {
  throw new Error("Source must be an HTTP(S) URL without credentials or query parameters");
}
const base = source.href.replace(/\/$/, "");
const out = resolve(values.out);
try { await stat(out); throw new Error("Refusing to overwrite an existing immutable capture: " + out); }
catch (error) { if (error.code !== "ENOENT") throw error; }
const rawManifest = await readFile(values["raw-manifest"]);
const sha = (bytes) => createHash("sha256").update(bytes).digest("hex");
async function get(path) {
  const response = await fetch(base + path, { signal: AbortSignal.timeout(120_000) });
  if (!response.ok) throw new Error(`GET ${path}: ${response.status}`);
  const bytes = Buffer.from(await response.arrayBuffer());
  return { bytes, value: JSON.parse(bytes.toString()) };
}
const index = await get("/history");
const ix = index.value;
if (!Array.isArray(ix.hours) || !ix.hours.length || !Number.isFinite(Date.parse(ix.oldest_minute)) ||
    !Number.isFinite(Date.parse(ix.newest_minute)) || Date.parse(ix.newest_minute) < Date.parse(ix.oldest_minute)) {
  throw new Error("History has no valid recorded window");
}
if (new Set(ix.hours).size !== ix.hours.length) throw new Error("Duplicate history hours");
const files = new Map([["history/index.json", index.bytes]]);
let fixes = 0, gapMinutes = 0, observedMinutes = 0;
for (const hour of ix.hours) {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:00:00Z$/.test(hour)) throw new Error("Invalid UTC hour");
  const key = hour.slice(0, 13);
  const { bytes, value: h } = await get("/history/" + key);
  if (h.schema !== "history-hour.v1" || h.hour_utc !== hour ||
      h.sampling !== "newest_fix_per_vehicle_per_minute" || h.minutes?.length !== 60 || !Array.isArray(h.vehicles)) {
    throw new Error("Invalid history-hour contract: " + key);
  }
  const cols = h.fixes;
  const n = cols?.v?.length;
  if (!Number.isInteger(n) || !["v", "t", "lat", "lon", "spd"].every(c => Array.isArray(cols[c]) && cols[c].length === n)) {
    throw new Error("Mismatched fix columns: " + key);
  }
  for (let i = 0; i < n; i++) {
    if (!Number.isInteger(cols.v[i]) || cols.v[i] < 0 || cols.v[i] >= h.vehicles.length ||
        !Number.isInteger(cols.t[i]) || cols.t[i] < 0 || cols.t[i] > 3599 ||
        !Number.isInteger(cols.lat[i]) || Math.abs(cols.lat[i]) > 90e5 ||
        !Number.isInteger(cols.lon[i]) || Math.abs(cols.lon[i]) > 180e5 ||
        !Number.isInteger(cols.spd[i]) || cols.spd[i] < -1) {
      throw new Error("Invalid recorded fix: " + key + " index " + i);
    }
  }
  for (const minute of h.minutes) {
    const f = minute.feeds;
    if (!f || !["ok", "failed", "provider_not_ok", "gap", "missing"].every(k => Number.isInteger(f[k]) && f[k] >= 0)) {
      throw new Error("Invalid minute coverage: " + key);
    }
    if (f.ok + f.provider_not_ok === 0) gapMinutes++;
    else observedMinutes++;
  }
  fixes += n;
  files.set("history/" + key + ".json", bytes);
}
if (!fixes || !observedMinutes) throw new Error("Refusing an empty replay");
const snapshot = await get("/vehicles/latest");
if (snapshot.value.as_of_minute !== ix.newest_minute || !Array.isArray(snapshot.value.coverage)) {
  throw new Error("Snapshot and frozen history do not describe the same last minute");
}
// Check the mutable index once more; never package a rolling API window across an hour switch.
const checked = await get("/history");
if (sha(index.bytes) !== sha(checked.bytes)) throw new Error("History index moved during export; retry");
files.set("snapshot.json", snapshot.bytes);
const provenance = {
  schema: "replay-capture.v1", capture: basename(out), exported_at: new Date().toISOString(),
  source: base, raw_manifest_sha256: sha(rawManifest), raw_manifest_name: basename(values["raw-manifest"]),
  oldest_minute: ix.oldest_minute, newest_minute: ix.newest_minute, fixes,
  observed_minutes: observedMinutes, gap_minutes_in_hour_files: gapMinutes,
  sampling: "newest_fix_per_vehicle_per_minute",
  notes: "Real recorded positions. Missing coverage remains missing. Partial hour boundaries are preserved.",
};
files.set("capture.json", Buffer.from(JSON.stringify(provenance, null, 2) + "\n"));
const temp = out + ".tmp-" + process.pid;
await mkdir(temp, { recursive: true });
const lines = [];
for (const [name, bytes] of files) {
  const target = resolve(temp, name);
  await mkdir(resolve(target, ".."), { recursive: true });
  await writeFile(target, bytes);
  if (sha(await readFile(target)) !== sha(bytes)) throw new Error("Written checksum mismatch: " + name);
  lines.push(sha(bytes) + "  " + name);
}
await writeFile(resolve(temp, "SHA256SUMS"), lines.sort().join("\n") + "\n");
await rename(temp, out);
console.log(JSON.stringify({ ...provenance, files: files.size, out }, null, 2));
