import type { MinuteSample } from "@/hooks/history-window";
import { formatCount } from "@/hooks/use-latest-vehicles";

// Vehicles per minute across the loaded window: one thin line over a faint area, from zero. A minute where
// no feed reported anything is a hatched column, never a drop to zero (invariant 6): the legend line in
// timeline.tsx says in words what the hatching means. Decoration for the pointer; the scrubber on top of it
// carries the same minutes as text (aria-valuetext).
//
// Minute i sits at i / (n - 1) of the width, which is where the scrubber's thumb is for that minute.
const HEIGHT = 48;

type Run = { from: number; to: number };

// Consecutive minutes with data, and consecutive gap minutes.
function runs(minutes: MinuteSample[], gap: boolean): Run[] {
  const out: Run[] = [];
  minutes.forEach((m, i) => {
    if (m.gap !== gap) return;
    const last = out[out.length - 1];
    if (last && last.to === i - 1) last.to = i;
    else out.push({ from: i, to: i });
  });
  return out;
}

export function chartMax(minutes: MinuteSample[]): number {
  return minutes.reduce((max, m) => (m.gap ? max : Math.max(max, m.vehicles)), 0);
}

export default function TimelineChart({ minutes, className = "" }: { minutes: MinuteSample[]; className?: string }) {
  const n = minutes.length;
  const span = Math.max(1, n - 1);
  const max = Math.max(1, chartMax(minutes));
  const y = (v: number) => HEIGHT - (v / max) * (HEIGHT - 2);

  const line: string[] = [];
  const area: string[] = [];
  for (const run of runs(minutes, false)) {
    const pts = minutes.slice(run.from, run.to + 1).map((m, k) => `${run.from + k},${y(m.vehicles).toFixed(2)}`);
    line.push(`M${pts.join("L")}`);
    area.push(`M${run.from},${HEIGHT}L${pts.join("L")}L${run.to},${HEIGHT}Z`);
  }

  return (
    <div aria-hidden="true" className={`pointer-events-none absolute inset-x-0.5 inset-y-0 ${className}`}>
      {runs(minutes, true).map((r) => (
        <div
          key={r.from}
          className="absolute inset-y-0 border-x border-neblina bg-[repeating-linear-gradient(135deg,var(--neblina)_0_1px,transparent_1px_5px)]"
          // Half a minute either side of the run, clamped to the track.
          style={{
            left: `${(Math.max(0, r.from - 0.5) / span) * 100}%`,
            width: `${((Math.min(span, r.to + 0.5) - Math.max(0, r.from - 0.5)) / span) * 100}%`,
            minWidth: 3,
          }}
        />
      ))}
      <svg viewBox={`0 0 ${span} ${HEIGHT}`} preserveAspectRatio="none" className="absolute inset-0 size-full overflow-visible">
        <path d={area.join("")} fill="color-mix(in srgb, var(--ceu) 28%, transparent)" />
        <path d={line.join("")} fill="none" stroke="var(--ceu)" strokeWidth="2" vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
      </svg>
    </div>
  );
}

export function chartMaxLabel(minutes: MinuteSample[]): string {
  return `máx. ${formatCount(chartMax(minutes))} ônibus por minuto`;
}
