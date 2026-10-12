import StatusStrip from "@/components/status-strip";
import VehicleView from "@/components/vehicle-view";

export default function Home() {
  return (
    <main className="mx-auto flex w-full max-w-7xl flex-1 flex-col gap-4 p-4">
      <header className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold">Transit Pulse — Rio de Janeiro</h1>
        <StatusStrip />
      </header>
      <VehicleView />
    </main>
  );
}
