"use client";

import "maplibre-gl/dist/maplibre-gl.css";
import { addProtocol, LngLatBounds, setWorkerUrl } from "maplibre-gl";
import { Protocol } from "pmtiles";
import { IconLayer } from "@deck.gl/layers";
import { useEffect, useRef } from "react";
import { Map, NavigationControl, useMap } from "react-map-gl/maplibre";
import { mapPadding } from "@/hooks/map-padding";
import { formatAgeSeconds, type VehicleRow } from "@/hooks/use-latest-vehicles";
import { useDeckOverlay } from "@/hooks/use-deck-overlay";
import type { ColorBy } from "@/hooks/dashboard-url";
import type { ColorScheme } from "@/hooks/use-color-scheme";
import { useReducedMotion } from "@/hooks/use-reduced-motion";
import { basemapStyle } from "./basemap";
import { hexToRgb, isRing, PALETTE, vehicleHex } from "./vehicle-style";

// Loaded only through live-map.tsx (next/dynamic, ssr: false), so `window` exists here.
// MapLibre 6 needs the worker URL set when a bundler is used (Turbopack form from MapLibre's docs).
setWorkerUrl(
  new URL("maplibre-gl/dist/maplibre-gl-worker.mjs", import.meta.url).toString(),
);
addProtocol("pmtiles", new Protocol().tile);

// Five white shapes side by side in one atlas, drawn once and tinted per vehicle (mask icons): a
// filled circle (bus), a filled square (BRT), their hollow rings (no speed), and a large ring for the
// selected bus. Real fixes only: nothing is drawn between them and positions snap on refresh.
const ICON_SIZE = 64;
const ICON_NAMES = ["circle", "square", "circle-ring", "square-ring", "select"] as const;
const ICON_MAPPING = Object.fromEntries(
  ICON_NAMES.map((name, i) => [name, { x: i * ICON_SIZE, y: 0, width: ICON_SIZE, height: ICON_SIZE, mask: true }]),
);

function drawAtlas(): string {
  const canvas = document.createElement("canvas");
  canvas.width = ICON_SIZE * ICON_NAMES.length;
  canvas.height = ICON_SIZE;
  const g = canvas.getContext("2d");
  if (!g) throw new Error("2D canvas is not available");
  g.fillStyle = "#fff";
  g.strokeStyle = "#fff";
  const centre = (i: number) => i * ICON_SIZE + ICON_SIZE / 2;

  const circle = (i: number, r: number) => {
    g.beginPath();
    g.arc(centre(i), ICON_SIZE / 2, r, 0, Math.PI * 2);
  };
  const square = (i: number, half: number) => {
    g.beginPath();
    g.rect(centre(i) - half, ICON_SIZE / 2 - half, half * 2, half * 2);
  };

  circle(0, 26);
  g.fill();
  square(1, 23);
  g.fill();
  g.lineWidth = 9;
  circle(2, 22);
  g.stroke();
  square(3, 20);
  g.stroke();
  g.lineWidth = 6;
  circle(4, 28);
  g.stroke();
  return canvas.toDataURL("image/png");
}

const ICON_ATLAS = drawAtlas();

// Additive blending for the dark scheme, so dense corridors glow: colour adds up where marks overlap.
const ADDITIVE = {
  blend: true,
  blendColorOperation: "add",
  blendColorSrcFactor: "src-alpha",
  blendColorDstFactor: "one",
  blendAlphaOperation: "add",
  blendAlphaSrcFactor: "one",
  blendAlphaDstFactor: "one-minus-src-alpha",
} as const;

// The glow is for "colorir por modo" on the dark scheme only. Colouring by speed needs five bins that
// stay tellable apart, and additive blending turns every dense corridor white-yellow, so speed uses
// normal blending in both schemes.
const glows = (colorBy: ColorBy, scheme: ColorScheme) => scheme === "dark" && colorBy === "modo";

// Opacity of a mark. With the glow it is below 1, so overlapping marks add up gradually instead of
// turning white at once, which keeps the colour and shape of marks in dense corridors. Not lower than
// this: the colour a single mark ends up with on the dark earth must stay at 3:1 or better (§6).
const GLOW_ALPHA = 190;
// With a line selected, every other vehicle drops to 15% of that.
const DIMMED_FACTOR = 0.15;

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

type VehicleLayerProps = {
  vehicles: VehicleRow[];
  linha: string | null;
  selected: VehicleRow | null;
  colorBy: ColorBy;
  scheme: ColorScheme;
  onSelectBus: (vehicle: string) => void;
};

function VehicleLayer({ vehicles, linha, selected, colorBy, scheme, onSelectBus }: VehicleLayerProps) {
  const selectedId = selected?.vehicle ?? null;
  useDeckOverlay({
    // Overlaid: deck.gl gets its own canvas above the map. Interleaved mode shares MapLibre's WebGL
    // context, and React Strict Mode's double mount in development then fails with "WebGL context
    // already attached to device". Nothing here needs to be depth-sorted into the basemap anyway.
    interleaved: false,
    getTooltip: tooltip,
    onClick: (info) => {
      const row = info.object as VehicleRow | undefined;
      if (row) onSelectBus(row.vehicle);
    },
    layers: [
      new IconLayer<VehicleRow>({
        id: "vehicles",
        data: vehicles,
        pickable: true,
        iconAtlas: ICON_ATLAS,
        iconMapping: ICON_MAPPING,
        getIcon: (d) => `${d.mode === "brt" ? "square" : "circle"}${isRing(d, colorBy) ? "-ring" : ""}`,
        getPosition: (d) => [d.lon, d.lat],
        getColor: (d) => {
          const dim = linha !== null && d.line !== linha && d.vehicle !== selectedId;
          return [...hexToRgb(vehicleHex(d, colorBy, scheme)), Math.round((glows(colorBy, scheme) ? GLOW_ALPHA : 255) * (dim ? DIMMED_FACTOR : 1))];
        },
        getSize: 12,
        sizeUnits: "pixels",
        updateTriggers: { getIcon: [colorBy], getColor: [colorBy, linha, scheme, selectedId] },
        parameters: glows(colorBy, scheme) ? ADDITIVE : { blend: true },
      }),
      new IconLayer<VehicleRow>({
        id: "selected-bus",
        data: selected ? [selected] : [],
        iconAtlas: ICON_ATLAS,
        iconMapping: ICON_MAPPING,
        getIcon: () => "select",
        getPosition: (d) => [d.lon, d.lat],
        getColor: [...hexToRgb(PALETTE[scheme].neutral), 255],
        getSize: 32,
        sizeUnits: "pixels",
        updateTriggers: { getColor: [scheme] },
      }),
    ],
  });
  return null;
}

type CameraProps = {
  vehicles: VehicleRow[];
  linha: string | null;
  selected: VehicleRow | null;
  follow: boolean;
  isDesktop: boolean;
  panelExpanded: boolean;
};

// Moves the camera. It's a separate component because it needs the map from react-map-gl's context.
// Each move below is an Effect because the map is an external system.
function Camera({ vehicles, linha, selected, follow, isDesktop, panelExpanded }: CameraProps) {
  const { current: map } = useMap();
  const reduced = useReducedMotion();
  const fittedLine = useRef<string | null>(null);
  const centredBus = useRef<string | null>(null);

  // Fit the line's vehicles when the selection changes, not on every refetch, so a reader who
  // panned away isn't pulled back. If the line has no vehicles yet (data still loading), wait.
  useEffect(() => {
    if (!map) return;
    if (linha === null) {
      fittedLine.current = null;
      return;
    }
    if (fittedLine.current === linha) return;
    const bounds = new LngLatBounds();
    let any = false;
    for (const v of vehicles) {
      if (v.line === linha) {
        bounds.extend([v.lon, v.lat]);
        any = true;
      }
    }
    if (!any) return;
    fittedLine.current = linha;
    map.fitBounds(bounds, {
      padding: mapPadding(isDesktop, panelExpanded, window.innerHeight),
      maxZoom: 15,
      duration: reduced ? 0 : 800,
    });
  }, [map, vehicles, linha, isDesktop, panelExpanded, reduced]);

  // A bus picked from the panel, or from a shared URL, may be off screen: bring it into view once.
  useEffect(() => {
    if (!map) return;
    if (selected === null) {
      centredBus.current = null;
      return;
    }
    if (centredBus.current === selected.vehicle) return;
    centredBus.current = selected.vehicle;
    if (map.getBounds().contains([selected.lon, selected.lat])) return;
    map.easeTo({
      center: [selected.lon, selected.lat],
      padding: mapPadding(isDesktop, panelExpanded, window.innerHeight),
      duration: reduced ? 0 : 600,
    });
  }, [map, selected, isDesktop, panelExpanded, reduced]);

  // Seguir: every new snapshot (a new row object) recentres on the bus. easeTo, or a jump with
  // reduced motion.
  useEffect(() => {
    if (!map || !follow || !selected) return;
    const target = {
      center: [selected.lon, selected.lat] as [number, number],
      padding: mapPadding(isDesktop, panelExpanded, window.innerHeight),
    };
    if (reduced) map.jumpTo(target);
    else map.easeTo({ ...target, duration: 500 });
  }, [map, follow, selected, isDesktop, panelExpanded, reduced]);

  return null;
}

type Props = VehicleLayerProps & CameraProps & { onUserPan: () => void };

export default function LiveMapCanvas({ onUserPan, ...props }: Props) {
  return (
    <Map
      initialViewState={{ longitude: -43.35, latitude: -22.9, zoom: 10 }}
      mapStyle={basemapStyle(props.scheme)}
      style={{ width: "100%", height: "100%" }}
      // Dragging the map by hand turns Seguir off. Moves we make (easeTo) are not drags.
      onDragStart={onUserPan}
    >
      <NavigationControl showCompass={false} />
      <VehicleLayer {...props} />
      <Camera {...props} />
    </Map>
  );
}
