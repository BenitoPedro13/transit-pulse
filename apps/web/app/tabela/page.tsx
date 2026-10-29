import type { Metadata } from "next";
import { Suspense } from "react";
import PlaybackProvider from "@/components/playback-provider";
import TabelaView from "@/components/tabela-view";

export const metadata: Metadata = {
  title: "Tabela de veículos — Transit Pulse",
  description: "Todos os veículos do último minuto gravado, em tabela: a alternativa ao mapa.",
};

export default function TabelaPage() {
  return (
    <main className="mx-auto flex w-full max-w-7xl flex-col gap-4 p-4">
      <Suspense fallback={<p className="text-sm text-neblina">Carregando…</p>}>
        <PlaybackProvider><TabelaView /></PlaybackProvider>
      </Suspense>
    </main>
  );
}
