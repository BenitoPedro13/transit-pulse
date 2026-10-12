// The five speed bins of "Colorir por velocidade". Half-open [from, to), in km/h, so a speed of exactly
// 50 is in the last bin and the legend's ranges never overlap. null is not a bin: a vehicle with no
// speed is drawn as a hollow ring and named "sem velocidade" in the legend (missing is not zero).
export const SPEED_BIN_UPPER = [3, 15, 30, 50] as const;

export type SpeedBin = 0 | 1 | 2 | 3 | 4;

// What the legend prints for each bin, slow to fast.
export const SPEED_BIN_LABELS = ["menos de 3", "3 a 15", "15 a 30", "30 a 50", "50 ou mais"] as const;

export function speedBin(kmh: number | null | undefined): SpeedBin | null {
  if (kmh === null || kmh === undefined || !Number.isFinite(kmh)) return null;
  const i = SPEED_BIN_UPPER.findIndex((upper) => kmh < upper);
  return (i === -1 ? SPEED_BIN_UPPER.length : i) as SpeedBin;
}
