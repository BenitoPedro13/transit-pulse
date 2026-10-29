import type { Metadata } from "next";
import { Suspense } from "react";
import PlaybackProvider from "@/components/playback-provider";
import TabelaView from "@/components/tabela-view";
import { ReplayDataProvider } from "@/hooks/data-source";

export const metadata: Metadata = {
  title: "Tabela da gravação — Transit Pulse",
  description: "Veículos de uma gravação real, em tabela. Não é ao vivo.",
};

export default function ReplayTablePage() {
  return (
    <main className="mx-auto flex w-full max-w-7xl flex-col gap-4 p-4">
      <ReplayDataProvider>
        <Suspense fallback={<p>Carregando gravação…</p>}>
          <PlaybackProvider><TabelaView /></PlaybackProvider>
        </Suspense>
      </ReplayDataProvider>
    </main>
  );
}
