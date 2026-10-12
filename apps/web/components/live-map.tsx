"use client";

import dynamic from "next/dynamic";

// The map touches `window` and WebGL, so it never renders on the server.
const LiveMapCanvas = dynamic(() => import("./live-map-canvas"), {
  ssr: false,
  loading: () => (
    <p className="p-4 text-sm text-zinc-700">Carregando o mapa…</p>
  ),
});

export default function LiveMap() {
  return (
    <div
      role="region"
      aria-label="Mapa de veículos"
      className="h-[60vh] min-h-80 w-full overflow-hidden rounded border border-zinc-300 bg-zinc-100 [&_.maplibregl-canvas:focus-visible]:outline-2 [&_.maplibregl-canvas:focus-visible]:outline-offset-[-2px] [&_.maplibregl-canvas:focus-visible]:outline-blue-700 [&_.maplibregl-canvas-container:focus-visible]:outline-2 [&_.maplibregl-canvas-container:focus-visible]:outline-offset-[-2px] [&_.maplibregl-canvas-container:focus-visible]:outline-blue-700"
    >
      <LiveMapCanvas />
    </div>
  );
}
