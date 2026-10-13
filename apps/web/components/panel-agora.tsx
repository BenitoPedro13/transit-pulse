"use client";

import { formatKmh, formatPercent, type FleetStats } from "@/hooks/use-fleet-stats";
import { formatCoverage, formatCount, type Snapshot } from "@/hooks/use-latest-vehicles";

// Each figure is a <dl> pair, so a screen reader hears the label and its value together.
function Stat({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="flex flex-col gap-0.5 rounded-lg border border-border bg-baia/60 px-3 py-2">
      <dt className="text-xs font-semibold tracking-wide text-neblina uppercase [font-stretch:75%]">{label}</dt>
      <dd className="text-xl leading-tight font-semibold text-calcadao">{value}</dd>
      {note && <dd className="text-xs text-neblina">{note}</dd>}
    </div>
  );
}

// Counts from the newest minute's snapshot, or in replay from the vehicles at the playhead (the
// coverage list is the live snapshot's, so it is left out there). Missing is not zero: a vehicle with no speed is counted
// apart ("sem velocidade") and is neither stopped nor moving.
type Props = {
  stats: FleetStats | undefined;
  snapshot: Snapshot | null;
  replay: boolean;
  onSelectLine: (line: string) => void;
};

export default function PanelAgora({ stats, snapshot, replay, onSelectLine }: Props) {
  if (!stats) return <p className="text-sm text-neblina">Carregando os números…</p>;

  return (
    <div className="flex flex-col gap-4">
      <dl className="grid grid-cols-2 gap-2">
        <Stat label="Ônibus" value={formatCount(stats.bus)} />
        <Stat label="BRT" value={formatCount(stats.brt)} />
        <Stat
          label="Em movimento"
          value={stats.movingShare === null ? "—" : formatPercent(stats.movingShare)}
          note={`sem velocidade: ${formatCount(stats.noSpeed)}`}
        />
        <Stat
          label="Mediana"
          value={stats.medianMovingKmh === null ? "—" : formatKmh(stats.medianMovingKmh)}
          note="dos que andam"
        />
      </dl>
      {stats.other > 0 && <p className="text-sm text-neblina">Outros modos: {formatCount(stats.other)} veículos.</p>}

      <section aria-labelledby="mais-onibus">
        <h2 id="mais-onibus" className="mb-1.5 text-sm font-semibold">
          {replay ? "Mais ônibus neste momento" : "Mais ônibus agora"}
        </h2>
        <ul className="flex flex-wrap gap-1.5">
          {stats.topLines.map((l) => (
            <li key={l.line}>
              <button
                type="button"
                onClick={() => onSelectLine(l.line)}
                className="rounded-md border border-border bg-card px-2.5 py-1 text-sm hover:bg-accent"
              >
                <span className="font-semibold">{l.line}</span> <span>· {l.count}</span>
              </button>
            </li>
          ))}
        </ul>
      </section>

      {replay && (
        <p className="text-xs text-neblina">
          Números do momento da reprodução: as posições mais recentes de cada ônibus, de até 5 minutos antes. A cobertura por operador é só do ao vivo.
        </p>
      )}

      {snapshot && !replay && (
        <section aria-labelledby="cobertura">
          <h2 id="cobertura" className="mb-1.5 text-sm font-semibold">
            Cobertura
          </h2>
          <ul className="flex list-disc flex-col gap-0.5 pl-5 text-xs text-neblina">
            {snapshot.coverage.map((c) => (
              <li key={c.operator}>{formatCoverage(c)}</li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
