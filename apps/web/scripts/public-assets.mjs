import { createHash } from "node:crypto";
import { mkdir, readFile, readdir, rename, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { gzipSync, gunzipSync } from "node:zlib";

const appRoot = fileURLToPath(new URL("../", import.meta.url));
const assetOrigin = "https://transit-pulse-tiles-860897618882.s3.us-east-1.amazonaws.com";
const maxBundleBytes = 16 * 1024 * 1024;
const maxExpandedBytes = 40 * 1024 * 1024;
export const sha256 = (bytes) => createHash("sha256").update(bytes).digest("hex");
const safePath = (path) => typeof path === "string" &&
  /^(gtfs|replay-data)\/[A-Za-z0-9_./-]+$/.test(path) &&
  path.split("/").every((part) => part && part !== "." && part !== "..");

export function unpack(bytes, release) {
  if (bytes.length > maxBundleBytes || bytes.length !== release.bytes || sha256(bytes) !== release.sha256) {
    throw new Error("Public asset bundle checksum/size mismatch");
  }
  const bundle = JSON.parse(gunzipSync(bytes, { maxOutputLength: maxExpandedBytes }));
  if (bundle.schema !== 1 || !Array.isArray(bundle.files) ||
      !Array.isArray(release.files) || bundle.files.length !== release.files.length) {
    throw new Error("Invalid public asset inventory");
  }
  const seen = new Set();
  return release.files.map((entry, i) => {
    const file = bundle.files[i];
    if (!safePath(entry.path) || seen.has(entry.path) || file.path !== entry.path) {
      throw new Error("Unsafe or duplicate public asset path");
    }
    seen.add(entry.path);
    const data = Buffer.from(file.base64, "base64");
    if (data.length !== entry.bytes || sha256(data) !== entry.sha256) {
      throw new Error("Public asset file checksum/size mismatch: " + entry.path);
    }
    return { path: entry.path, data };
  });
}

async function pack(outDir) {
  const files = [];
  async function collect(relative) {
    const dir = join(appRoot, "public", relative);
    for (const entry of (await readdir(dir, { withFileTypes: true })).sort((a, b) => a.name.localeCompare(b.name))) {
      const path = relative + "/" + entry.name;
      if (entry.isDirectory()) await collect(path);
      else if (entry.isFile() && safePath(path)) {
        const data = await readFile(join(appRoot, "public", path));
        files.push({ path, data });
      } else throw new Error("Unexpected public asset entry: " + path);
    }
  }
  await collect("gtfs");
  await collect("replay-data");
  if (!files.length) throw new Error("No public assets");
  const bytes = gzipSync(JSON.stringify({ schema: 1, files: files.map(({ path, data }) => ({ path, base64: data.toString("base64") })) }), { level: 9 });
  const hash = sha256(bytes);
  const release = {
    schema: 1,
    url: assetOrigin + "/web-releases/" + hash + ".json.gz",
    bytes: bytes.length,
    sha256: hash,
    files: files.map(({ path, data }) => ({ path, bytes: data.length, sha256: sha256(data) })),
  };
  unpack(bytes, release);
  await mkdir(outDir, { recursive: true });
  await writeFile(join(outDir, hash + ".json.gz"), bytes, { flag: "wx" });
  await writeFile(join(outDir, "assets-release.json"), JSON.stringify(release, null, 2) + "\n");
  console.log("Packed " + files.length + " public files into " + bytes.length + " bytes; release " + hash);
}

async function prepare(publicDir) {
  const release = JSON.parse(await readFile(join(appRoot, "assets-release.json"), "utf8"));
  if (release.schema !== 1 || release.url !== assetOrigin + "/web-releases/" + release.sha256 + ".json.gz") {
    throw new Error("Invalid pinned public asset release");
  }
  const response = await fetch(release.url, { signal: AbortSignal.timeout(60_000) });
  if (!response.ok) throw new Error("Public asset download answered " + response.status);
  let length = 0;
  const chunks = [];
  for await (const chunk of response.body) {
    length += chunk.length;
    if (length > maxBundleBytes) throw new Error("Public asset download too large");
    chunks.push(chunk);
  }
  // Validate every byte and path before writing any public files.
  const files = unpack(Buffer.concat(chunks), release);
  for (const file of files) {
    const path = join(publicDir, file.path);
    await mkdir(dirname(path), { recursive: true });
    await writeFile(path + ".download", file.data);
    await rename(path + ".download", path);
  }
  console.log("Verified and prepared " + files.length + " public assets (" + files.reduce((sum, file) => sum + file.data.length, 0) + " bytes)");
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  if (process.argv[2] === "--pack" && process.argv[3]) await pack(process.argv[3]);
  else if (!process.argv[2] || (process.argv[2] === "--public-dir" && process.argv[3])) {
    await prepare(process.argv[3] ?? join(appRoot, "public"));
  } else throw new Error("Usage: node scripts/public-assets.mjs [--pack output-dir | --public-dir public-dir]");
}
