// How much of the map the floating panels cover, so a fitted selection or a followed bus stays in
// the part you can see. Pure: the dashboard passes the layout state, the camera calls it when it moves.
export type MapPadding = { top: number; right: number; bottom: number; left: number };

// Heights of the phone panel, as a share of the viewport (components/dashboard.tsx).
export const PHONE_PANEL_COLLAPSED = 0.33;
export const PHONE_PANEL_EXPANDED = 0.7;

// Keep at least this share of the height free, or MapLibre refuses to fit ("cannot fit within canvas").
const MIN_FREE = 0.3;

export function mapPadding(isDesktop: boolean, panelExpanded: boolean, viewportHeight: number): MapPadding {
  if (isDesktop) {
    // The panel is 22 rem (352 px) wide with a 16 px margin; the letreiro sits at the top left.
    return { top: 96, right: 352 + 16 + 40, bottom: 72, left: 64 };
  }
  const panel = (panelExpanded ? PHONE_PANEL_EXPANDED : PHONE_PANEL_COLLAPSED) * viewportHeight;
  let top = 180; // letreiro, status and colour control stacked at the top
  let bottom = panel + 24;
  const budget = viewportHeight * (1 - MIN_FREE);
  if (top + bottom > budget) {
    const scale = budget / (top + bottom);
    top = Math.round(top * scale);
    bottom = Math.round(bottom * scale);
  }
  return { top, right: 32, bottom, left: 32 };
}
