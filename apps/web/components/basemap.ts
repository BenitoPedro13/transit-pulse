import { layers, namedFlavor, type Flavor } from "@protomaps/basemaps";
import type { StyleSpecification } from "maplibre-gl";
import type { ColorScheme } from "@/hooks/use-color-scheme";

// The basemap in each scheme: Protomaps' own flavour with the dominant colours pulled toward the
// page's tokens. Dark is "Guanabara at night" (blue-green, water darker than land); light is the
// stock light flavour with the same tint, and with the saturated park and water colours softened so
// the vehicle marks stay at 3:1 or better on them. Keys checked against @protomaps/basemaps 5.7
// (`Flavor`). The marks' contrast against these colours is in docs/tasks/TASK-dashboard.md §6.
const TINT: Record<ColorScheme, Partial<Flavor>> = {
  dark: {
    background: "#0b1f26",
    earth: "#0e252d",
    water: "#081a21",
    buildings: "#0a1c22",
    park_a: "#12322f",
    park_b: "#12322f",
    wood_a: "#12302d",
    wood_b: "#12302d",
    scrub_a: "#12302d",
    scrub_b: "#12302d",
    minor_a: "#143038",
    minor_b: "#143038",
    other: "#143038",
    minor_service: "#143038",
    major: "#183843",
    link: "#183843",
    highway: "#1d4350",
    city_label: "#8fa9ae",
    subplace_label: "#6f898f",
    roads_label_major: "#7a979d",
    roads_label_minor: "#5f7c83",
    ocean_label: "#6f8f9c",
  },
  light: {
    background: "#dde6e4",
    earth: "#e7edeb",
    water: "#b9dce3",
    buildings: "#d5dedc",
    park_a: "#cfe0d6",
    park_b: "#bfdccb",
    wood_a: "#cfe0d3",
    wood_b: "#bfdcc6",
    scrub_a: "#d3e0da",
    scrub_b: "#bfdcd0",
    minor_a: "#f2f5f4",
  },
};

export function basemapFlavor(scheme: ColorScheme): Flavor {
  return { ...namedFlavor(scheme), ...TINT[scheme] };
}

// One style object per scheme, built once: react-map-gl calls setStyle whenever the prop's identity
// changes, so a new object per render would reload the style on every render.
const styles = new Map<ColorScheme, StyleSpecification>();

export function basemapStyle(scheme: ColorScheme): StyleSpecification {
  let style = styles.get(scheme);
  if (!style) {
    style = buildStyle(scheme);
    styles.set(scheme, style);
  }
  return style;
}

// Absolute URL: the pmtiles protocol handler fetches it outside the page's base URL. The sprite
// follows the scheme so icons (and their halos) match the map.
function buildStyle(scheme: ColorScheme): StyleSpecification {
  return {
    version: 8,
    glyphs: "https://protomaps.github.io/basemaps-assets/fonts/{fontstack}/{range}.pbf",
    sprite: `https://protomaps.github.io/basemaps-assets/sprites/v4/${scheme}`,
    sources: {
      protomaps: {
        type: "vector",
        url: `pmtiles://${window.location.origin}/tiles/rio.pmtiles`,
        attribution:
          '<a href="https://protomaps.com">Protomaps</a> © <a href="https://openstreetmap.org">OpenStreetMap</a>',
      },
    },
    layers: layers("protomaps", basemapFlavor(scheme), { lang: "pt" }),
  };
}
