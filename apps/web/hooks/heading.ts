const POINTS = ["norte", "nordeste", "leste", "sudeste", "sul", "sudoeste", "oeste", "noroeste"] as const;

// 0° is north and angles grow clockwise, as in the feed. Each point covers 45°, centred on it:
// 337.5° up to (not including) 22.5° is "norte". An angle of exactly 22.5° rounds up to "nordeste".
export function compassPoint(deg: number): (typeof POINTS)[number] {
  const normalised = ((deg % 360) + 360) % 360;
  return POINTS[Math.round(normalised / 45) % 8];
}

// "135°, sudeste", or "sem direção" when the feed gave none (missing is not 0° north).
export function formatHeading(deg: number | null): string {
  if (deg === null || !Number.isFinite(deg)) return "sem direção";
  return `${Math.round(deg)}°, ${compassPoint(deg)}`;
}
