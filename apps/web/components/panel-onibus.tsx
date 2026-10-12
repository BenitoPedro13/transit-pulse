"use client";

import { Button } from "@/components/ui/button";
import { Toggle } from "@/components/ui/toggle";
import { formatHeading } from "@/hooks/heading";
import { formatAgeSeconds, formatClock, formatNullable, type VehicleRow } from "@/hooks/use-latest-vehicles";
import type { SelectedVehicle } from "@/hooks/use-selected-vehicle";
import { LineSign } from "./letreiro";

type Props = {
  vehicle: string;
  selected: SelectedVehicle;
  follow: boolean;
  onFollowChange: (follow: boolean) => void;
  onClear: () => void;
};

// An arrow pointing where the bus heads (0° is north, clockwise). Decoration: the text beside it says
// the same in words.
function HeadingArrow({ deg }: { deg: number }) {
  return (
    <svg aria-hidden="true" width="16" height="16" viewBox="0 0 16 16" className="inline-block shrink-0" style={{ transform: `rotate(${deg}deg)` }}>
      <path d="M8 1.5 L12.5 13 L8 10.5 L3.5 13 Z" fill="currentColor" />
    </svg>
  );
}

function Card({ row, follow, onFollowChange }: { row: VehicleRow; follow: boolean; onFollowChange: (f: boolean) => void }) {
  return (
    <div className="flex flex-col gap-3">
      <LineSign>{row.line ?? "Sem linha"}</LineSign>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 text-sm">
        <dt className="text-neblina">Veículo</dt>
        <dd className="font-semibold">{row.vehicle}</dd>
        <dt className="text-neblina">Operador</dt>
        <dd>{row.operatorLabel}</dd>
        <dt className="text-neblina">Velocidade</dt>
        <dd>{row.speed_kmh === null ? "sem velocidade" : formatNullable(row.speed_kmh, "km/h")}</dd>
        <dt className="text-neblina">Direção</dt>
        <dd className="flex items-center gap-1.5">
          {row.heading_deg !== null && <HeadingArrow deg={row.heading_deg} />}
          {formatHeading(row.heading_deg)}
        </dd>
        <dt className="text-neblina">Posição</dt>
        <dd>{row.ageSeconds === null ? "idade desconhecida" : `visto há ${formatAgeSeconds(row.ageSeconds)}`}</dd>
      </dl>
      <Toggle pressed={follow} onPressedChange={onFollowChange} className="self-start">
        Seguir
      </Toggle>
    </div>
  );
}

export default function PanelOnibus({ vehicle, selected, follow, onFollowChange, onClear }: Props) {
  return (
    <div className="flex flex-col gap-3">
      {selected.kind === "found" && <Card row={selected.row} follow={follow} onFollowChange={onFollowChange} />}
      {selected.kind === "missing" && (
        <>
          <p className="text-sm font-semibold">Ônibus {vehicle}</p>
          <p className="text-sm">Não aparece no último minuto gravado ({formatClock(selected.asOfMinute)}).</p>
          <Toggle pressed={false} disabled className="self-start">
            Seguir
          </Toggle>
        </>
      )}
      {(selected.kind === "loading" || selected.kind === "unavailable") && (
        <p className="text-sm text-neblina">{selected.kind === "loading" ? "Carregando o ônibus…" : "Sem dados: a API não respondeu."}</p>
      )}
      <Button variant="outline" size="sm" onClick={onClear} className="self-start">
        Limpar ônibus
      </Button>
    </div>
  );
}
