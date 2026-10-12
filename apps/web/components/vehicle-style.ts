import { speedBin } from "@/hooks/speed-bins";
import type { ColorBy } from "@/hooks/dashboard-url";
import type { ColorScheme } from "@/hooks/use-color-scheme";

// One place for what a vehicle looks like, shared by the map layer and the legend. deck.gl can't read
// CSS variables, so these are the same hex values as app/globals.css (vehicle-style.test.ts fails if
// they drift). Every colour is at least 3:1 against the basemap's earth, water, buildings and parks in
// its scheme (WCAG 1.4.11, ratios in docs/tasks/TASK-dashboard.md §6).
//
// Mode is carried by the shape (circle = bus, square = BRT), so colour only adds to it (WCAG 1.4.1).
// Speed is carried by five bins with numbers in the legend, and a missing speed by a hollow ring.
export type Palette = {
  bus: string;
  brt: string;
  // The ring of the selected bus and the hollow "sem velocidade" marks.
  neutral: string;
  // Slow to fast.
  speed: [string, string, string, string, string];
};

export const PALETTE: Record<ColorScheme, Palette> = {
  light: {
    bus: "#0a6fa8",
    brt: "#c2412f",
    neutral: "#0b1f26",
    speed: ["#a2650a", "#8c5000", "#763c00", "#612900", "#4c1500"],
  },
  dark: {
    bus: "#7fd4ff",
    brt: "#ff6b5a",
    neutral: "#e8f0ee",
    speed: ["#af711f", "#c78533", "#de9b4b", "#f0b370", "#ffcd97"],
  },
};

export type Rgb = [number, number, number];

export function hexToRgb(hex: string): Rgb {
  return [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16)) as Rgb;
}

type Colored = { mode: string; speed_kmh: number | null };

// The colour of one vehicle's mark. In "velocidade" a missing speed gets the neutral colour and is
// drawn as a ring (see isRing), never as the slowest bin.
export function vehicleHex(v: Colored, colorBy: ColorBy, scheme: ColorScheme): string {
  const p = PALETTE[scheme];
  if (colorBy === "velocidade") {
    const bin = speedBin(v.speed_kmh);
    return bin === null ? p.neutral : p.speed[bin];
  }
  return v.mode === "brt" ? p.brt : p.bus;
}

export function isRing(v: Colored, colorBy: ColorBy): boolean {
  return colorBy === "velocidade" && speedBin(v.speed_kmh) === null;
}
