"use client";

import { useState } from "react";
import { filterByLine, formatCount, useLatestVehicles, type VehicleRow } from "@/hooks/use-latest-vehicles";
import Legend from "./legend";
import LiveMap from "./live-map";
import VehicleTable from "./vehicle-table";

const NO_ROWS: VehicleRow[] = [];

// The line filter lives here, above both children, so the map and the table always show the same
// vehicles.
export default function VehicleView() {
  const { data, isPending } = useLatestVehicles();
  const [line, setLine] = useState("");
  const [pageIndex, setPageIndex] = useState(0);

  const all = data?.rows ?? NO_ROWS;
  const rows = filterByLine(all, line);
  const filtered = line.trim() !== "";

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <div className="flex flex-col gap-2 lg:sticky lg:top-4 lg:self-start">
        <LiveMap vehicles={rows} />
        <Legend />
      </div>
      <section aria-labelledby="tabela-titulo" className="flex min-w-0 flex-col gap-3">
        <h2 id="tabela-titulo" className="text-lg font-semibold">
          Lista de veículos
        </h2>
        <div className="flex flex-col gap-1">
          <label htmlFor="filtro-linha" className="text-sm font-medium">
            Filtrar por linha
          </label>
          <input
            id="filtro-linha"
            type="search"
            autoComplete="off"
            value={line}
            onChange={(e) => {
              setLine(e.target.value);
              setPageIndex(0);
            }}
            className="rounded border border-zinc-500 px-2 py-1 text-sm focus-visible:outline-2 focus-visible:outline-blue-700"
          />
        </div>
        {isPending ? (
          <p className="text-sm text-zinc-700">Carregando veículos…</p>
        ) : data === undefined ? (
          <p className="text-sm text-zinc-700">Sem dados de veículos: a API não respondeu.</p>
        ) : rows.length === 0 ? (
          <p className="text-sm text-zinc-700">
            {filtered
              ? `Nenhum veículo da linha “${line.trim()}” no último minuto gravado.`
              : "Nenhum veículo no último minuto gravado."}
          </p>
        ) : (
          <VehicleTable
            rows={rows}
            pageIndex={pageIndex}
            onPageIndexChange={setPageIndex}
            caption={
              filtered
                ? `${formatCount(rows.length)} de ${formatCount(all.length)} veículos, linhas que começam com “${line.trim()}”`
                : `${formatCount(all.length)} veículos no último minuto gravado`
            }
          />
        )}
      </section>
    </div>
  );
}
