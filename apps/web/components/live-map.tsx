"use client";

import dynamic from "next/dynamic";
import type { ComponentProps } from "react";

// The map touches `window` and WebGL, so it never renders on the server.
const LiveMapCanvas = dynamic(() => import("./live-map-canvas"), {
  ssr: false,
  loading: () => <p className="p-4 text-sm text-neblina">Carregando o mapa…</p>,
});

// Full-bleed: the dashboard puts everything else over it.
export default function LiveMap(props: ComponentProps<typeof LiveMapCanvas>) {
  return (
    <div role="region" aria-label="Mapa de veículos" className="absolute inset-0 bg-baia">
      <LiveMapCanvas {...props} />
    </div>
  );
}
