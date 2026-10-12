"use client";

import { Pause, Play } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { minuteText } from "@/hooks/history-window";
import { usePlaybackControls } from "@/hooks/use-playback";
import { formatClock, formatCount } from "@/hooks/use-latest-vehicles";
import { useReducedMotion } from "@/hooks/use-reduced-motion";
import { SPEEDS } from "@/store/playback";
import TimelineChart, { chartMaxLabel } from "./timeline-chart";

// The time machine's controls (docs/tasks/TASK-time-machine.md §2.5): play, speed, "Só posições reais",
// the way back to live, and a scrubber laid over a chart of vehicles per minute. On a phone it is the top
// row of the bottom panel (play, scrubber, AO VIVO); expanding the panel adds the rest.
export default function Timeline({ expanded, className = "" }: { expanded: boolean; className?: string }) {
  const c = usePlaybackControls();
  const reduced = useReducedMotion();
  const live = c.mode === "live";
  const w = c.window;

  const minute = w?.minutes[c.index];
  const clock = minute ? formatClock(new Date(minute.startMs).toISOString()) : "";
  const valueText = minute ? minuteText(minute, clock, formatCount) : "Histórico não carregado";

  // The hour marks of the chart: where the local clock reads :00.
  const span = w ? Math.max(1, w.minutes.length - 1) : 1;
  const marks = w
    ? w.minutes.flatMap((m, i) => (new Date(m.startMs).getMinutes() === 0 ? [{ i, label: formatClock(new Date(m.startMs).toISOString()) }] : []))
    : [];
  const hasGap = w?.minutes.some((m) => m.gap) ?? false;

  const compact = "max-lg:group-data-[expanded=false]/tl:hidden";

  return (
    <section
      aria-label="Linha do tempo"
      data-expanded={expanded}
      className={`group/tl pointer-events-auto flex flex-wrap items-center gap-x-3 gap-y-2 text-calcadao lg:rounded-xl lg:border lg:border-borda lg:bg-morro lg:p-3 lg:shadow-lg ${className}`}
    >
      <Button
        variant="outline"
        size="icon"
        className="order-1 shrink-0"
        disabled={!c.ready}
        aria-label={c.playing ? "Pausar" : "Reproduzir"}
        onClick={c.togglePlay}
      >
        {c.playing ? <Pause aria-hidden="true" /> : <Play aria-hidden="true" />}
      </Button>

      <ToggleGroup
        type="single"
        size="sm"
        spacing={0}
        aria-label="Velocidade"
        value={String(c.speed)}
        disabled={reduced}
        onValueChange={(v) => v && c.setSpeed(Number(v) as (typeof SPEEDS)[number])}
        className={`max-lg:order-10 lg:order-2 ${compact}`}
      >
        {SPEEDS.map((s) => (
          <ToggleGroupItem key={s} value={String(s)} aria-label={`Velocidade ${s} vezes`}>
            {s}×
          </ToggleGroupItem>
        ))}
      </ToggleGroup>

      <div className={`flex items-center gap-2 max-lg:order-11 lg:order-2 ${compact}`}>
        <Switch id="so-posicoes-reais" checked={!c.smoothing} onCheckedChange={(on) => c.setSmoothing(!on)} />
        <label htmlFor="so-posicoes-reais" className="text-sm">
          Só posições reais
        </label>
      </div>

      {/* What the playhead is on, and what the map is showing (the labels that keep replay honest). */}
      <div className={`min-w-0 text-xs text-neblina max-lg:order-12 max-lg:basis-full lg:order-3 lg:flex-1 ${compact}`}>
        <p className="truncate text-sm font-semibold text-calcadao">
          {live ? "Ao vivo" : "Reprodução"} · {valueText}
        </p>
        {w && (
          <p className="flex flex-wrap gap-x-3">
            <span>{chartMaxLabel(w.minutes)}</span>
            {hasGap && (
              <span className="flex items-center gap-1.5">
                <span aria-hidden="true" className="inline-block h-3 w-4 border-x border-neblina bg-[repeating-linear-gradient(135deg,var(--neblina)_0_1px,transparent_1px_4px)]" />
                falha na coleta
              </span>
            )}
            {!live && <span>{c.smoothing ? "Movimento suavizado entre posições reais (1 por minuto)" : "Só posições reais: 1 por minuto por ônibus"}</span>}
            {reduced && <span>Reprodução de 1 minuto por segundo (movimento reduzido)</span>}
          </p>
        )}
      </div>

      <Button
        variant={live ? "outline" : "default"}
        size="sm"
        className="order-3 shrink-0 font-semibold max-lg:group-data-[expanded=true]/tl:ml-auto lg:order-4"
        disabled={live}
        aria-label={live ? "Ao vivo" : "Voltar ao vivo"}
        onClick={c.goLive}
      >
        {live ? <span aria-hidden="true">● </span> : null}AO VIVO
      </Button>

      {/* The track: on a phone it sits between play and AO VIVO; on a desktop, and on an expanded phone, it
          takes a row of its own. */}
      <div className="order-2 min-w-0 max-lg:flex-1 max-lg:group-data-[expanded=true]/tl:order-20 max-lg:group-data-[expanded=true]/tl:basis-full lg:order-20 lg:basis-full">
        {w ? (
          <>
            <div className="relative h-8 max-lg:group-data-[expanded=true]/tl:h-10 lg:h-10">
              <TimelineChart minutes={w.minutes} className={compact} />
              <div aria-hidden="true" className="absolute inset-x-0 bottom-0 border-b border-borda" />
              <Slider
                min={0}
                max={c.lastIndex}
                step={1}
                value={[c.index]}
                onValueChange={([i]) => c.seekIndex(i)}
                className="absolute inset-0 h-full"
                thumbProps={{
                  "aria-label": "Minuto da reprodução",
                  "aria-valuetext": valueText,
                  className: "h-full w-1.5 rounded-sm border-calcadao bg-calcadao",
                }}
              />
            </div>
            <div aria-hidden="true" className={`relative mt-0.5 h-4 text-xs text-neblina ${compact}`}>
              {marks.map((m) => (
                <span key={m.i} className="absolute -translate-x-1/2" style={{ left: `${(m.i / span) * 100}%` }}>
                  {m.label}
                </span>
              ))}
            </div>
          </>
        ) : (
          <p role="status" className="text-sm text-neblina">
            {c.status === "error" ? "Histórico indisponível agora." : "Carregando o histórico…"}
          </p>
        )}
      </div>
    </section>
  );
}
