"use client";

import { ChevronUp } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { letreiroText } from "@/hooks/letreiro-text";
import { useColorScheme } from "@/hooks/use-color-scheme";
import { useDashboardParams } from "@/hooks/use-dashboard-params";
import { fleetStats, formatKmh, formatPercent, useFleetStats } from "@/hooks/use-fleet-stats";
import { useIsDesktop } from "@/hooks/use-is-desktop";
import { formatCount, useLatestVehicles, type VehicleRow } from "@/hooks/use-latest-vehicles";
import { selectFromRows, useSelectedVehicle, type SelectedVehicle } from "@/hooks/use-selected-vehicle";
import { useVehicleStatus } from "@/hooks/use-vehicle-status";
import { toLineIndex, useLineIndex, useVehiclesOfLine, vehiclesOfLine } from "@/hooks/use-lines";
import { useReplayRows, useReplayView } from "@/hooks/use-playback";
import { routeView, useLineRoutes } from "@/hooks/use-line-routes";
import ColorLegend from "./color-legend";
import Letreiro from "./letreiro";
import LiveMap from "./live-map";
import PanelAgora from "./panel-agora";
import PanelLinhas from "./panel-linhas";
import PanelOnibus from "./panel-onibus";
import PlaybackClock from "./playback-clock";
import PlaybackUrlSync from "./playback-url-sync";
import StatusStrip from "./status-strip";
import Timeline from "./timeline";

type Tab = "agora" | "linhas" | "onibus";

const NO_ROWS: VehicleRow[] = [];

// The whole screen: a full-bleed map with everything else floating over it. It owns the pieces of state
// that more than one child needs and passes them down as props: what is selected (in the URL), which
// panel tab is open, Seguir, and whether the phone panel is expanded.
export default function Dashboard() {
  const params = useDashboardParams();
  const { data } = useLatestVehicles();
  const status = useVehicleStatus();
  const view = useReplayView();
  const replayRows = useReplayRows(view);
  const replay = view.kind !== "live";

  // Live, the panels read the newest snapshot; in replay, the vehicles at the playhead's minute.
  const liveStats = useFleetStats();
  const liveIndex = useLineIndex();
  const liveSelected = useSelectedVehicle(params.onibus);
  const liveLineVehicles = useVehiclesOfLine(params.linha);
  const stats = replayRows ? fleetStats(replayRows) : liveStats;
  const lineIndex = replayRows ? toLineIndex(replayRows) : liveIndex;
  const lineVehicles = replayRows ? (params.linha ? vehiclesOfLine(replayRows, params.linha) : NO_ROWS) : liveLineVehicles;
  const selected: SelectedVehicle = replayRows
    ? view.kind === "ready"
      ? selectFromRows(replayRows, params.onibus, new Date(view.minute.startMs).toISOString())
      : params.onibus === null
        ? { kind: "none" }
        : { kind: view.kind === "loading" ? "loading" : "unavailable" }
    : liveSelected;
  const scheme = useColorScheme();
  const isDesktop = useIsDesktop();

  const [tab, setTab] = useState<Tab>(params.onibus ? "onibus" : params.linha ? "linhas" : "agora");
  const [follow, setFollow] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const routes = useLineRoutes(params.linha);
  const [patternChoice, setPatternChoice] = useState<{ line: string | null; id: string | null }>({ line: null, id: null });
  const routeData = routes.kind === "ready" ? routes.data : undefined;
  const patternId = patternChoice.line === params.linha ? patternChoice.id : null;
  const referenceRoute = useMemo(() => routeView(routeData, patternId), [routeData, patternId]);

  // A bus that drops out of the newest minute can't be followed. Said while rendering, not in an
  // Effect, so Seguir is off in the same render that shows "Não aparece".
  if (follow && selected.kind !== "found") setFollow(false);

  // The bus tab exists only while a bus is named in the URL.
  const activeTab: Tab = tab === "onibus" && !params.onibus ? "agora" : tab;
  const selectedRow = selected.kind === "found" ? selected.row : null;

  // Every handler that changes the selection also fixes the tab and Seguir, in the same event.
  function selectLine(line: string) {
    params.selectLine(line);
    setTab("linhas");
    setFollow(false);
  }
  function clearLine() {
    params.clearLine();
  }
  function selectBus(vehicle: string) {
    params.selectBus(vehicle);
    setTab("onibus");
    setFollow(false);
  }
  function clearBus() {
    params.clearBus();
    setTab(params.linha ? "linhas" : "agora");
    setFollow(false);
  }
  function changeTab(next: string) {
    setTab(next as Tab);
    setExpanded(true);
  }

  const text = letreiroText({
    status: status.kind,
    replay:
      view.kind === "live"
        ? null
        : view.kind === "ready"
          ? { state: "ready", minuteStart: view.minute.startMs, vehicles: view.minute.vehicles, gap: view.minute.gap }
          : { state: view.kind, minuteStart: 0, vehicles: 0, gap: false },
    asOfMinute: data?.snapshot.as_of_minute ?? null,
    total: data?.rows.length ?? 0,
    linha: params.linha,
    lineCount: lineVehicles.length,
    selected,
    onibus: params.onibus,
  });

  // The one line the collapsed phone panel shows.
  let summary = "Carregando…";
  if (replay && view.kind !== "ready") {
    summary = "Reprodução — carregando o histórico";
  } else if (selectedRow) {
    summary = `${selectedRow.line ?? "Sem linha"} · ${selectedRow.vehicle} · ${selectedRow.speed_kmh === null ? "sem velocidade" : formatKmh(selectedRow.speed_kmh)}`;
  } else if (params.onibus) {
    summary = `Ônibus ${params.onibus}`;
  } else if (params.linha) {
    summary = `Linha ${params.linha} · ${formatCount(lineVehicles.length)} ônibus`;
  } else if (stats) {
    summary = `${stats.movingShare === null ? "—" : formatPercent(stats.movingShare)} em movimento · mediana ${stats.medianMovingKmh === null ? "—" : formatKmh(stats.medianMovingKmh)}`;
  }

  const when = replay ? "neste momento da reprodução" : "no último minuto gravado";
  const tableHref = params.linha ? `/tabela?linha=${encodeURIComponent(params.linha)}` : "/tabela";

  return (
    <main
      className="relative h-svh w-full overflow-hidden"
      // Where the phone panel's top edge is, so the map's zoom buttons sit above it (globals.css).
      style={{ "--panel-h": expanded ? "70svh" : "33svh" } as React.CSSProperties}
    >
      {/* pointer-events-none: the empty space between the floating pieces still drags the map. */}
      <PlaybackClock />
      <PlaybackUrlSync />

      <div className="pointer-events-none absolute top-3 right-3 left-3 z-10 flex flex-col items-start gap-2 lg:top-4 lg:right-auto lg:left-4 lg:max-w-[46rem]">
        <Letreiro text={text} />
        <div className="flex w-full items-start justify-between gap-2">
          <StatusStrip />
          <Link
            href={tableHref}
            className="pointer-events-auto shrink-0 rounded-md border border-borda bg-morro px-3 py-1.5 text-sm font-medium text-calcadao hover:bg-morro-alto lg:fixed lg:right-4 lg:bottom-[11rem]"
          >
            Ver como tabela ▸
          </Link>
        </div>
      </div>

      <section
        aria-label="Painel"
        data-expanded={expanded}
        className="absolute z-20 flex flex-col gap-2 rounded-t-xl border border-borda bg-morro p-3 text-calcadao shadow-lg max-lg:inset-x-0 max-lg:bottom-0 max-lg:max-h-[33svh] max-lg:data-[expanded=true]:h-[70svh] max-lg:data-[expanded=true]:max-h-none lg:top-4 lg:right-4 lg:max-h-[calc(100svh-16rem)] lg:w-[22rem] lg:rounded-xl"
      >
        <div className="flex shrink-0 items-center justify-between gap-2 lg:hidden">
          <p className="truncate text-sm">{summary}</p>
          <Button
            variant="ghost"
            size="sm"
            className="h-11 shrink-0"
            aria-label={expanded ? "Recolher painel" : "Expandir painel"}
            aria-expanded={expanded}
            aria-controls="painel-conteudo"
            onClick={() => setExpanded(!expanded)}
          >
            {expanded ? "Recolher" : "Expandir"}
            <ChevronUp aria-hidden="true" className={expanded ? "rotate-180" : ""} />
          </Button>
        </div>

        {/* The mobile collapse control stays above the timeline and scrolling tabs; desktop spans the bottom. */}
        <Timeline expanded={expanded} className="lg:fixed lg:inset-x-4 lg:bottom-9" />

        <Tabs value={activeTab} onValueChange={changeTab} className={expanded ? "min-h-0 flex-1" : "lg:min-h-0 lg:flex-1"}>
          <TabsList aria-label="Painel" className="shrink-0">
            <TabsTrigger value="agora">Agora</TabsTrigger>
            <TabsTrigger value="linhas">Linhas</TabsTrigger>
            {params.onibus && <TabsTrigger value="onibus">Ônibus</TabsTrigger>}
          </TabsList>

          <div id="painel-conteudo" className={`min-h-0 flex-1 overflow-y-auto pr-1 ${expanded ? "" : "max-lg:hidden"}`}>
            <TabsContent value="agora">
              <PanelAgora stats={stats} snapshot={status.snapshot} replay={replay} onSelectLine={selectLine} />
            </TabsContent>
            <TabsContent value="linhas">
              <PanelLinhas
                index={lineIndex}
                buses={lineVehicles}
                when={when}
                linha={params.linha}
                onibus={params.onibus}
                onSelectLine={selectLine}
                onClearLine={clearLine}
                onSelectBus={selectBus}
                routes={routes}
                route={referenceRoute}
                onPattern={(id) => setPatternChoice({ line: params.linha, id })}
              />
            </TabsContent>
            {params.onibus && (
              <TabsContent value="onibus">
                <PanelOnibus
                  vehicle={params.onibus}
                  selected={selected}
                  follow={follow}
                  onFollowChange={setFollow}
                  onClear={clearBus}
                  replay={replay}
                />
              </TabsContent>
            )}
          </div>
        </Tabs>

        {/* On a phone this is the panel's footer, so it stays visible when the panel is collapsed; on
            a desktop it floats at the bottom left of the map (fixed to the viewport). */}
        <ColorLegend cor={params.cor} onChange={params.setCor} className="lg:fixed lg:bottom-[11rem] lg:left-4" />
      </section>

      {referenceRoute && (
        <p className="pointer-events-none absolute left-3 z-10 max-w-[calc(100%-1.5rem)] rounded-md border border-borda bg-morro px-2 py-1 text-xs text-calcadao max-lg:bottom-[calc(var(--panel-h)+0.5rem)] lg:bottom-[16.5rem] lg:left-4">
          Trajeto de referência · GTFS estático
        </p>
      )}

      {/* Last in the source, so the keyboard reaches the heading, the panel and the legend before the
          map's own controls. Painting order is by z-index, not source order. */}
      <LiveMap
        vehicles={data?.rows ?? NO_ROWS}
        linha={params.linha}
        selected={selectedRow}
        selectedId={params.onibus}
        colorBy={params.cor}
        scheme={scheme}
        follow={follow}
        isDesktop={isDesktop}
        panelExpanded={expanded}
        onSelectBus={selectBus}
        onUserPan={() => setFollow(false)}
        route={referenceRoute}
        routePending={routes.kind === "loading"}
      />
    </main>
  );
}
