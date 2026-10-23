import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// In dev, send /ws to the gateways behind nginx (docker compose up, port 80).
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/ws': { target: process.env.VITE_GATEWAY ?? 'ws://localhost', ws: true },
    },
  },
  test: { environment: 'jsdom' },
})
