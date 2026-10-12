import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { PALETTE, hexToRgb, isRing, vehicleHex } from "./vehicle-style";

// globals.css is the source of the tokens; deck.gl needs the same values in JS. This fails if one
// changes without the other.
const css = readFileSync(new URL("../app/globals.css", import.meta.url), "utf8");
const lightBlock = css.slice(css.indexOf(":root {"), css.indexOf("@media (prefers-color-scheme: dark) {"));
const darkBlock = css.slice(css.indexOf("@media (prefers-color-scheme: dark) {"), css.indexOf("@theme inline"));

function token(block: string, name: string): string {
  const m = block.match(new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})`));
  if (!m) throw new Error(`token --${name} not found`);
  return m[1].toLowerCase();
}

describe("vehicle colours", () => {
  for (const [scheme, block] of [["light", lightBlock], ["dark", darkBlock]] as const) {
    it(`match the ${scheme} tokens in globals.css`, () => {
      const p = PALETTE[scheme];
      expect(p.bus).toBe(token(block, "ceu"));
      expect(p.brt).toBe(token(block, "coral"));
      expect(p.neutral).toBe(token(block, "calcadao"));
      expect(p.speed).toEqual([0, 1, 2, 3, 4].map((i) => token(block, `vel-${i}`)));
    });
  }

  it("colours by mode or by speed bin, and a missing speed is neutral and a ring", () => {
    const bus = { mode: "bus", speed_kmh: 31 };
    const brt = { mode: "brt", speed_kmh: null };
    expect(vehicleHex(bus, "modo", "dark")).toBe(PALETTE.dark.bus);
    expect(vehicleHex(brt, "modo", "dark")).toBe(PALETTE.dark.brt);
    expect(vehicleHex(bus, "velocidade", "light")).toBe(PALETTE.light.speed[3]);
    expect(vehicleHex(brt, "velocidade", "light")).toBe(PALETTE.light.neutral);
    expect(isRing(brt, "velocidade")).toBe(true);
    expect(isRing(brt, "modo")).toBe(false);
    expect(isRing({ mode: "bus", speed_kmh: 0 }, "velocidade")).toBe(false);
  });

  it("converts hex to rgb", () => {
    expect(hexToRgb("#0a6fa8")).toEqual([10, 111, 168]);
  });
});
