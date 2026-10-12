import { Suspense } from "react";
import Dashboard from "@/components/dashboard";
import Letreiro from "@/components/letreiro";

// useSearchParams (the view lives in the URL) makes the tree up to the nearest Suspense boundary
// render in the browser, so the boundary is here and the fallback is the sign saying it is loading.
export default function Home() {
  return (
    <Suspense
      fallback={
        <main className="h-svh p-4">
          <Letreiro text="CARREGANDO" />
        </main>
      }
    >
      <Dashboard />
    </Suspense>
  );
}
