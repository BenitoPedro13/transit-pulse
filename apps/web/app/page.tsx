import LiveMap from "@/components/live-map";

export default function Home() {
  return (
    <main className="mx-auto flex w-full max-w-7xl flex-1 flex-col gap-4 p-4">
      <header>
        <h1 className="text-2xl font-semibold">Transit Pulse — Rio de Janeiro</h1>
        <p role="status" className="text-sm text-zinc-700">
          Sem dados de veículos ainda
        </p>
      </header>
      <LiveMap />
      <section aria-labelledby="tabela-titulo">
        <h2 id="tabela-titulo" className="text-lg font-semibold">
          Lista de veículos
        </h2>
        <p className="text-sm text-zinc-700">
          A lista de veículos chega junto com a API.
        </p>
      </section>
    </main>
  );
}
