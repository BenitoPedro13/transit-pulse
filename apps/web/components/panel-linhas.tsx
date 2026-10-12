"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { formatCount } from "@/hooks/use-latest-vehicles";
import { matchLines, useLineIndex, useVehiclesOfLine, type LineEntry } from "@/hooks/use-lines";

type Props = {
  linha: string | null;
  onibus: string | null;
  onSelectLine: (line: string) => void;
  onClearLine: () => void;
  onSelectBus: (vehicle: string) => void;
};

const MODE_WORD: Record<string, string> = { bus: "ônibus", brt: "BRT" };

function ModeShape({ mode }: { mode: string }) {
  return (
    <svg aria-hidden="true" width="14" height="14" viewBox="0 0 16 16" className="shrink-0">
      {mode === "brt" ? <rect x="2" y="2" width="12" height="12" fill="var(--coral)" /> : <circle cx="8" cy="8" r="6.5" fill="var(--ceu)" />}
    </svg>
  );
}

function LineGroup({ title, lines, linha, onSelect }: { title: string; lines: LineEntry[]; linha: string | null; onSelect: (line: string) => void }) {
  if (lines.length === 0) return null;
  return (
    <section aria-label={title}>
      <h3 className="mb-1 text-xs font-semibold tracking-wide text-neblina uppercase [font-stretch:75%]">{title}</h3>
      <ul className="grid grid-cols-2 gap-1">
        {lines.map((l) => (
          <li key={l.line}>
            <button
              type="button"
              aria-pressed={l.line === linha}
              onClick={() => onSelect(l.line)}
              className="flex w-full items-center gap-1.5 rounded-md border border-border bg-card px-2 py-1 text-left text-sm hover:bg-accent aria-pressed:border-primary aria-pressed:bg-primary aria-pressed:text-primary-foreground"
            >
              <ModeShape mode={l.mode} />
              <span className="font-semibold">{l.line}</span>
              <span className="text-xs opacity-80">
                {MODE_WORD[l.mode] ?? l.mode} · {formatCount(l.count)}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}

export default function PanelLinhas({ linha, onibus, onSelectLine, onClearLine, onSelectBus }: Props) {
  const index = useLineIndex();
  const buses = useVehiclesOfLine(linha);
  const [query, setQuery] = useState("");

  if (!index) return <p className="text-sm text-neblina">Carregando as linhas…</p>;

  const matches = matchLines(index.lines, query);
  const brt = matches.filter((l) => l.mode === "brt");
  const others = matches.filter((l) => l.mode !== "brt");
  const q = query.trim();

  return (
    <div className="flex flex-col gap-3">
      {linha !== null && (
        <section aria-label={`Linha ${linha}`} className="flex flex-col gap-2 rounded-lg border border-border bg-baia/60 p-2.5">
          <div className="flex items-center justify-between gap-2">
            <h2 className="text-sm font-semibold">
              Linha {linha}
              <span className="font-normal text-neblina"> · {formatCount(buses.length)} ônibus</span>
            </h2>
            <Button variant="outline" size="sm" onClick={onClearLine}>
              Limpar linha
            </Button>
          </div>
          {buses.length === 0 ? (
            <p className="text-sm text-neblina">A linha {linha} não está no último minuto gravado.</p>
          ) : (
            <ul className="flex flex-wrap gap-1">
              {buses.map((b) => (
                <li key={b.vehicle}>
                  <button
                    type="button"
                    aria-pressed={b.vehicle === onibus}
                    onClick={() => onSelectBus(b.vehicle)}
                    className="rounded-md border border-border bg-card px-2 py-1 text-xs hover:bg-accent aria-pressed:border-primary aria-pressed:bg-primary aria-pressed:text-primary-foreground"
                  >
                    {b.vehicle}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      <div className="flex flex-col gap-1">
        <label htmlFor="buscar-linha" className="text-sm font-medium">
          Buscar linha
        </label>
        <input
          id="buscar-linha"
          type="search"
          autoComplete="off"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="h-9 rounded-lg border border-border bg-card px-2.5 text-sm text-calcadao"
        />
      </div>

      {matches.length === 0 ? (
        <p className="text-sm text-neblina">Nenhuma linha começa com “{q}” no último minuto gravado.</p>
      ) : (
        <div className="flex flex-col gap-3">
          <LineGroup title="BRT" lines={brt} linha={linha} onSelect={onSelectLine} />
          <LineGroup title="Ônibus" lines={others} linha={linha} onSelect={onSelectLine} />
        </div>
      )}

      {index.unidentified > 0 && q === "" && (
        <p className="text-xs text-neblina">
          {formatCount(index.unidentified)} {index.unidentified === 1 ? "veículo sem linha identificada" : "veículos sem linha identificada"}.
        </p>
      )}
    </div>
  );
}
