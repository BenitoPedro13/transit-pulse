"use client";

import { useMemo } from "react";
import { Layer, Source } from "react-map-gl/maplibre";
import type { FilterSpecification } from "maplibre-gl";
import type { RouteView } from "@/hooks/use-line-routes";
import type { ColorScheme } from "@/hooks/use-color-scheme";
import { PALETTE } from "./vehicle-style";

const PATH: FilterSpecification = ["==", ["get", "kind"], "path"];
const STOP: FilterSpecification = ["==", ["get", "kind"], "stop"];
const LINE_LAYOUT = { "line-cap": "round", "line-join": "round" } as const;

// Reference geometry is static MapLibre data beneath the deck.gl animation canvas.
// Changing the playhead does not update or redraw these layers.
export default function RouteReference({ view, scheme }: { view: RouteView | null; scheme: ColorScheme }) {
  const data = useMemo(() => view ? ({
    type: "FeatureCollection" as const,
    features: [
      { type: "Feature" as const, properties: { kind: "path" }, geometry: { type: "LineString" as const, coordinates: view.shape.path } },
      ...view.stops.map((s) => ({ type: "Feature" as const, properties: { kind: "stop", name: s.name }, geometry: { type: "Point" as const, coordinates: s.position } })),
    ],
  }) : null, [view]);
  const paint = useMemo(() => {
    const ink = PALETTE[scheme].neutral;
    const base = scheme === "dark" ? "#0b1f26" : "#eef2f1";
    return {
      outline: { "line-color": base, "line-width": 7 },
      line: { "line-color": ink, "line-width": 3 },
      stops: { "circle-radius": 4, "circle-color": base, "circle-stroke-color": ink, "circle-stroke-width": 2 },
    };
  }, [scheme]);
  if (!data) return null;
  return (
    <Source id="reference-route-source" type="geojson" data={data}>
      <Layer id="reference-route-outline" type="line" filter={PATH} layout={LINE_LAYOUT} paint={paint.outline} />
      <Layer id="reference-route" type="line" filter={PATH} layout={LINE_LAYOUT} paint={paint.line} />
      <Layer id="reference-stops" type="circle" filter={STOP} paint={paint.stops} />
    </Source>
  );
}
