import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';

// Same "@" alias as rsbuild.config.ts, so tests can import app modules.
export default defineConfig({
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
});
