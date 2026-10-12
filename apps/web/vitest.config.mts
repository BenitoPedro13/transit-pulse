import { defineConfig } from "vitest/config";

// Pure functions only (hooks/*.test.ts): no DOM needed.
export default defineConfig({
  test: { environment: "node", include: ["hooks/**/*.test.ts"] },
});
