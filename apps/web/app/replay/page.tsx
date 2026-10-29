import type { Metadata } from "next";
import { Suspense } from "react";
import Dashboard from "@/components/dashboard";
import Letreiro from "@/components/letreiro";
import PlaybackProvider from "@/components/playback-provider";
import { ReplayDataProvider } from "@/hooks/data-source";

export const metadata: Metadata = {
  title: "Reprodução gravada — Transit Pulse",
  description: "Explore uma gravação real dos ônibus do Rio. Posições e lacunas preservadas; não é ao vivo.",
};

export default function ReplayPage() {
  return (
    <ReplayDataProvider>
      <Suspense fallback={<main className="h-svh p-4"><Letreiro text="CARREGANDO GRAVAÇÃO" /></main>}>
        <PlaybackProvider><Dashboard /></PlaybackProvider>
      </Suspense>
    </ReplayDataProvider>
  );
}
