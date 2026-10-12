import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

// Pure functions only (hooks/*.test.ts, components/*.test.ts): no DOM needed. `@/` is tsconfig's alias.
export default defineConfig({
  resolve: { alias: { "@": fileURLToPath(new URL(".", import.meta.url)) } },
  test: { environment: "node", include: ["hooks/**/*.test.ts", "components/**/*.test.ts"] },
});
