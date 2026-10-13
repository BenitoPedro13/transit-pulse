"use client";

import type { RoutesState, RouteView } from "@/hooks/use-line-routes";
import { feedDate, patternLabel } from "@/hooks/use-line-routes";
import { Button } from "./ui/button";

export default function PanelRoute({ state, view, onPattern }: { state: RoutesState; view: RouteView | null; onPattern: (id: string) => void }) {
  if (state.kind === "none") return null;
  if (state.kind === "loading") return <p role="status" className="text-sm text-neblina">Carregando o trajeto de referência…</p>;
  if (state.kind === "missing") return <p className="text-sm text-neblina">Sem trajeto no GTFS para esta linha.</p>;
  if (state.kind === "error") return (
    <div className="flex flex-col items-start gap-2">
      <p role="status" className="text-sm text-neblina">Não foi possível carregar o trajeto de referência.</p>
      <Button variant="outline" className="min-h-11" onClick={state.retry}>Tentar novamente</Button>
    </div>
  );
  if (state.kind !== "ready" || !view) return null;
  const date = new Intl.DateTimeFormat("pt-BR", { timeZone: "America/Sao_Paulo" }).format(new Date(state.feed.fetched_at));
  const modified = Date.parse(state.feed.modified_at);
  const published = Number.isFinite(modified) ? new Intl.DateTimeFormat("pt-BR", { timeZone: "UTC" }).format(modified) : "não informada";
  return (
    <section aria-label="Trajeto e paradas" className="flex flex-col gap-2 border-t border-borda pt-3">
      <h3 className="text-sm font-semibold">Trajeto de referência</h3>
      <p className="text-sm text-neblina">{view.route.name}</p>
      <label htmlFor="trajeto-destino" className="text-sm font-medium">Destino e variante</label>
      <select
        id="trajeto-destino"
        value={view.pattern.id}
        onChange={(e) => onPattern(e.target.value)}
        className="min-h-11 w-full min-w-0 rounded-md border border-borda bg-card px-2 text-sm text-calcadao"
      >
        {state.data.patterns.map((p) => <option key={p.id} value={p.id}>{patternLabel(state.data, p)}</option>)}
      </select>
      <details>
        <summary className="flex min-h-11 cursor-pointer items-center text-sm underline underline-offset-4">{view.stops.length} paradas · ver lista</summary>
        <ol aria-label="Paradas do trajeto" className="flex flex-col gap-2 border-l-2 border-borda pl-3 text-sm">
          {view.stops.map((s, i) => <li key={`${i}-${s.id}`}><span className="mr-2 text-neblina">{i + 1}.</span>{s.name}</li>)}
        </ol>
      </details>
      <details className="text-xs text-neblina">
        <summary className="flex min-h-11 cursor-pointer items-center underline underline-offset-4">Sobre este trajeto · GTFS estático</summary>
        <div className="flex flex-col gap-2 pb-2">
          <p>Capturado em {date}. Vigência declarada: {feedDate(state.feed.start_date)} a {feedDate(state.feed.end_date)}.</p>
          <p>Última alteração do arquivo: {published}.</p>
          <p>Referência do arquivo publicado. Não confirma o trajeto percorrido pelos ônibus nem a configuração da linha na data da reprodução.</p>
          <p>{state.feed.attribution} · CC BY 4.0.</p>
        </div>
      </details>
    </section>
  );
}
