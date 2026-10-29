import assert from "node:assert/strict";
import { test } from "node:test";
import { gzipSync } from "node:zlib";
import { sha256, unpack } from "./public-assets.mjs";

function fixture(path = "gtfs/index.json") {
  const data = Buffer.from('{"schema":"real-fixture"}');
  const bytes = gzipSync(JSON.stringify({ schema: 1, files: [{ path, base64: data.toString("base64") }] }));
  const release = { bytes: bytes.length, sha256: sha256(bytes), files: [{ path, bytes: data.length, sha256: sha256(data) }] };
  return { bytes, release, data };
}
test("accepts complete verified assets without changing bytes", () => {
  const { bytes, release, data } = fixture();
  assert.deepEqual(unpack(bytes, release), [{ path: "gtfs/index.json", data }]);
});
test("rejects truncated downloads and file corruption", () => {
  const { bytes, release } = fixture();
  assert.throws(() => unpack(bytes.subarray(1), release), /checksum/);
  release.files[0].sha256 = "0".repeat(64);
  assert.throws(() => unpack(bytes, release), /file checksum/);
});
test("rejects traversal even when all checksums match", () => {
  const { bytes, release } = fixture("gtfs/../../package.json");
  assert.throws(() => unpack(bytes, release), /Unsafe/);
});
test("rejects incomplete and duplicate inventories", () => {
  const { bytes, release } = fixture();
  release.files.push(release.files[0]);
  assert.throws(() => unpack(bytes, release), /inventory/);
  const bundle = gzipSync(JSON.stringify({ schema: 1, files: [0, 1].map(() => ({ path: "gtfs/index.json", base64: Buffer.from('{"schema":"real-fixture"}').toString("base64") })) }));
  assert.throws(() => unpack(bundle, { ...release, bytes: bundle.length, sha256: sha256(bundle) }), /duplicate/);
});
