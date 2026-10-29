"use client";

import Link from "next/link";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { selectionHref, useDataSource } from "@/hooks/data-source";
import { useReplayRows, useReplayView } from "@/hooks/use-playback";
import { useDashboardParams } from "@/hooks/use-dashboard-params";
import { filterByLine, formatCount, useLatestVehicles, type VehicleRow } from "@/hooks/use-latest-vehicles";
import StatusStrip from "./status-strip";
import VehicleTable from "./vehicle-table";

const NO_ROWS: VehicleRow[] = [];

// The non-map alternative to the dashboard (WCAG 2.2 AA; CLAUDE.md invariant 7): every vehicle of the
// newest minute, sortable, in a table. ?linha= is the dashboard's selected line, so a link from the map
// shows the same vehicles; the box below narrows further by line prefix.
export default function TabelaView() {
  const { data, isPending } = useLatestVehicles();
  const params = useDashboardParams();
  const { linha, clearLine } = params;
  const source = useDataSource();
  const view = useReplayView();
  const replayRows = useReplayRows(view);
  const [prefix, setPrefix] = useState("");
  const [pageIndex, setPageIndex] = useState(0);

  const all = replayRows ?? data?.rows ?? NO_ROWS;
  const ofLine = linha === null ? all : all.filter((r) => r.line === linha);
  const rows = filterByLine(ofLine, prefix);
  const filtered = prefix.trim() !== "";
  const backHref = selectionHref(source.mapHref, params);

  return (
    <>
      <header className="flex flex-col gap-2">
        <Link href={backHref} className="w-fit rounded-md border border-borda bg-morro px-3 py-1.5 text-sm font-medium hover:bg-morro-alto">
          ◂ Voltar ao mapa
        </Link>
        <h1 className="text-2xl font-semibold">{source.kind === "replay" ? "Veículos na gravação" : "Veículos no último minuto gravado"}</h1>
        <StatusStrip />
      </header>

      {linha !== null && (
        <p className="flex flex-wrap items-center gap-2 text-sm">
          Mostrando só a linha <strong>{linha}</strong>.
          <Button variant="outline" size="sm" onClick={clearLine}>
            Mostrar todas as linhas
          </Button>
        </p>
      )}

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
            value={prefix}
            onChange={(e) => {
              setPrefix(e.target.value);
              setPageIndex(0);
            }}
            className="h-9 max-w-xs rounded-lg border border-border bg-card px-2.5 text-sm"
          />
        </div>
        {isPending ? (
          <p className="text-sm text-neblina">Carregando veículos…</p>
        ) : data === undefined ? (
          <p className="text-sm text-neblina">Sem dados de veículos: a API não respondeu.</p>
        ) : rows.length === 0 ? (
          <p className="text-sm text-neblina">
            {filtered
              ? `Nenhum veículo da linha “${prefix.trim()}” no último minuto gravado.`
              : linha !== null
                ? `Nenhum veículo da linha “${linha}” no último minuto gravado.`
                : "Nenhum veículo no último minuto gravado."}
          </p>
        ) : (
          <VehicleTable
            rows={rows}
            pageIndex={pageIndex}
            onPageIndexChange={setPageIndex}
            caption={
              filtered
                ? `${formatCount(rows.length)} de ${formatCount(ofLine.length)} veículos, linhas que começam com “${prefix.trim()}”`
                : `${formatCount(ofLine.length)} veículos${linha !== null ? ` da linha ${linha}` : ""} no último minuto gravado`
            }
          />
        )}
      </section>
    </>
  );
}
