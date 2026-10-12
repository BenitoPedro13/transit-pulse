import { MapLibreOverlay, type MapLibreOverlayProps } from "@deck.gl/maplibre";
import { useControl } from "react-map-gl/maplibre";

// deck.gl layers drawn by a MapLibre control, on a canvas of their own above the map. Must be
// called by a component rendered inside <Map>. setProps during render is the pattern deck.gl
// documents for react-map-gl: the overlay is an external object, not React state.
export function useDeckOverlay(props: MapLibreOverlayProps) {
  const overlay = useControl<MapLibreOverlay>(() => new MapLibreOverlay(props));
  overlay.setProps({
    ...props,
    // deck.gl makes its canvas a tab stop with no focus ring (WCAG 2.4.7) once it has started,
    // and the canvas only repeats what the vehicle table says. So after it loads, take it out of
    // the keyboard and assistive-technology order. (Earlier than onLoad, deck overwrites this.)
    onLoad: () => {
      props.onLoad?.();
      const canvas = overlay.getCanvas();
      canvas?.setAttribute("tabindex", "-1");
      canvas?.setAttribute("aria-hidden", "true");
    },
  });
}
