"use client";

import type { ColorBy } from "@/hooks/dashboard-url";
import { SPEED_BIN_LABELS } from "@/hooks/speed-bins";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

type Props = { cor: ColorBy; onChange: (cor: ColorBy) => void; className?: string };

const SHAPE_SIZE = 16;

// The map's shapes, drawn from the same tokens as the layer. `fill` is a CSS colour or a variable.
function Shape({ kind, fill, ring = false }: { kind: "circle" | "square"; fill: string; ring?: boolean }) {
  const paint = ring ? { fill: "none", stroke: fill, strokeWidth: 2.5 } : { fill };
  return (
    <svg aria-hidden="true" width={SHAPE_SIZE} height={SHAPE_SIZE} viewBox="0 0 16 16" className="shrink-0">
      {kind === "circle" ? <circle cx="8" cy="8" r={ring ? 5.5 : 6.5} {...paint} /> : <rect x={ring ? 3 : 2} y={ring ? 3 : 2} width={ring ? 10 : 12} height={ring ? 10 : 12} {...paint} />}
    </svg>
  );
}

// "Colorir por" and the legend for what the map shows now. The shapes always carry the mode; the
// colour is either the mode (modo) or one of five speed bins with their ranges in text (velocidade).
export default function ColorLegend({ cor, onChange, className = "" }: Props) {
  return (
    <div className={`pointer-events-auto flex flex-col gap-2 text-calcadao lg:max-w-[22rem] lg:rounded-lg lg:border lg:border-borda lg:bg-morro lg:p-2.5 ${className}`}>
      <div className="flex items-center gap-2">
        <span id="colorir-por" className="text-sm font-medium">
          Colorir por
        </span>
        <ToggleGroup
          type="single"
          size="sm"
          spacing={0}
          value={cor}
          // Radix reports "" when the pressed item is pressed again; one of the two always stays on.
          onValueChange={(v) => v && onChange(v as ColorBy)}
          aria-labelledby="colorir-por"
        >
          <ToggleGroupItem value="modo">modo</ToggleGroupItem>
          <ToggleGroupItem value="velocidade">velocidade</ToggleGroupItem>
        </ToggleGroup>
      </div>

      <ul aria-label="Legenda do mapa" className="flex flex-wrap gap-x-3 gap-y-1 text-xs lg:text-sm">
        {cor === "modo" ? (
          <>
            <li className="flex items-center gap-1.5">
              <Shape kind="circle" fill="var(--ceu)" />
              Círculo: ônibus
            </li>
            <li className="flex items-center gap-1.5">
              <Shape kind="square" fill="var(--coral)" />
              Quadrado: BRT
            </li>
          </>
        ) : (
          <>
            <li className="flex w-full items-center gap-3">
              <span className="flex items-center gap-1.5">
                <Shape kind="circle" fill="var(--calcadao)" />
                Círculo: ônibus
              </span>
              <span className="flex items-center gap-1.5">
                <Shape kind="square" fill="var(--calcadao)" />
                Quadrado: BRT
              </span>
            </li>
            <li className="text-neblina">Velocidade em km/h:</li>
            {SPEED_BIN_LABELS.map((label, i) => (
              <li key={label} className="flex items-center gap-1.5">
                <Shape kind="circle" fill={`var(--vel-${i})`} />
                {label}
              </li>
            ))}
            <li className="flex items-center gap-1.5">
              <Shape kind="circle" fill="var(--calcadao)" ring />
              Anel: sem velocidade
            </li>
          </>
        )}
      </ul>
    </div>
  );
}
