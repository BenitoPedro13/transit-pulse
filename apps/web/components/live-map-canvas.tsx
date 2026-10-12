"use client";

import "maplibre-gl/dist/maplibre-gl.css";
import { addProtocol, setWorkerUrl, type StyleSpecification } from "maplibre-gl";
import { layers, namedFlavor } from "@protomaps/basemaps";
import { Protocol } from "pmtiles";
import { IconLayer } from "@deck.gl/layers";
import { Map, NavigationControl } from "react-map-gl/maplibre";
import { formatAgeSeconds, type VehicleRow } from "@/hooks/use-latest-vehicles";
import { useDeckOverlay } from "@/hooks/use-deck-overlay";
import { BRT_COLOR, BUS_COLOR, OUTLINE_COLOR } from "./vehicle-style";

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

// Two icons side by side in one atlas, drawn once: a circle for buses and a square for BRT.
// Real fixes only: nothing is drawn between them and positions snap on refresh.
const ICON_SIZE = 64;
const ICON_MAPPING = {
  circle: { x: 0, y: 0, width: ICON_SIZE, height: ICON_SIZE },
  square: { x: ICON_SIZE, y: 0, width: ICON_SIZE, height: ICON_SIZE },
};

function drawAtlas(): string {
  const canvas = document.createElement("canvas");
  canvas.width = ICON_SIZE * 2;
  canvas.height = ICON_SIZE;
  const g = canvas.getContext("2d");
  if (!g) throw new Error("2D canvas is not available");
  g.lineWidth = 7;
  g.strokeStyle = OUTLINE_COLOR;
  g.fillStyle = BUS_COLOR;
  g.beginPath();
  g.arc(ICON_SIZE / 2, ICON_SIZE / 2, 24, 0, Math.PI * 2);
  g.fill();
  g.stroke();
  g.fillStyle = BRT_COLOR;
  g.beginPath();
  g.rect(ICON_SIZE + 9, 9, 46, 46);
  g.fill();
  g.stroke();
  return canvas.toDataURL("image/png");
}

const ICON_ATLAS = drawAtlas();

function tooltip({ object }: { object?: VehicleRow | null }) {
  if (!object) return null;
  return {
    text: [
      `Linha ${object.line ?? "—"}`,
      `Veículo ${object.vehicle}`,
      object.operatorLabel,
      `Última posição há ${formatAgeSeconds(object.ageSeconds)}`,
    ].join("\n"),
  };
}

function VehicleLayer({ vehicles }: { vehicles: VehicleRow[] }) {
  useDeckOverlay({
    // Overlaid: deck.gl gets its own canvas above the map. Interleaved mode shares MapLibre's WebGL
    // context, and React Strict Mode's double mount in development then fails with "WebGL context
    // already attached to device". Nothing here needs to be depth-sorted into the basemap anyway.
    interleaved: false,
    getTooltip: tooltip,
    layers: [
      new IconLayer<VehicleRow>({
        id: "vehicles",
        data: vehicles,
        pickable: true,
        iconAtlas: ICON_ATLAS,
        iconMapping: ICON_MAPPING,
        getIcon: (d) => (d.mode === "brt" ? "square" : "circle"),
        getPosition: (d) => [d.lon, d.lat],
        getSize: 14,
        sizeUnits: "pixels",
      }),
    ],
  });
  return null;
}

export default function LiveMapCanvas({ vehicles }: { vehicles: VehicleRow[] }) {
  return (
    <Map
      initialViewState={{ longitude: -43.35, latitude: -22.9, zoom: 10 }}
      mapStyle={style}
      style={{ width: "100%", height: "100%" }}
    >
      <NavigationControl showCompass={false} />
      <VehicleLayer vehicles={vehicles} />
    </Map>
  );
}
