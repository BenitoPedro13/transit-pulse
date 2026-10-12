"use client";

import "maplibre-gl/dist/maplibre-gl.css";
import { addProtocol, setWorkerUrl, type StyleSpecification } from "maplibre-gl";
import { layers, namedFlavor } from "@protomaps/basemaps";
import { Protocol } from "pmtiles";
import { Map, NavigationControl } from "react-map-gl/maplibre";

// Loaded only through live-map.tsx (next/dynamic, ssr: false), so `window` exists here.
// MapLibre 6 needs the worker URL set when a bundler is used (Turbopack form from MapLibre's docs).
setWorkerUrl(
  new URL("maplibre-gl/dist/maplibre-gl-worker.mjs", import.meta.url).toString(),
);
addProtocol("pmtiles", new Protocol().tile);

// Absolute URL: the protocol handler fetches it outside the page's base URL.
const style: StyleSpecification = {
  version: 8,
  glyphs:
    "https://protomaps.github.io/basemaps-assets/fonts/{fontstack}/{range}.pbf",
  sprite: "https://protomaps.github.io/basemaps-assets/sprites/v4/light",
  sources: {
    protomaps: {
      type: "vector",
      url: `pmtiles://${window.location.origin}/tiles/rio.pmtiles`,
      attribution:
        '<a href="https://protomaps.com">Protomaps</a> © <a href="https://openstreetmap.org">OpenStreetMap</a>',
    },
  },
  layers: layers("protomaps", namedFlavor("light"), { lang: "pt" }),
};

export default function LiveMapCanvas() {
  return (
    <Map
      initialViewState={{ longitude: -43.35, latitude: -22.9, zoom: 10 }}
      mapStyle={style}
      style={{ width: "100%", height: "100%" }}
    >
      <NavigationControl showCompass={false} />
    </Map>
  );
}
