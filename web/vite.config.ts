import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// The Go service serves the built app and the API from one origin. In dev,
// Vite proxies the API and signed media to a locally running service.
const backend = process.env.DREAMER_BACKEND_URL ?? 'http://localhost:8080'

export default defineConfig({
  plugins: [react()],
  base: '/',
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    sourcemap: false,
    // Every Telegram WebView we target supports modulepreload natively.
    modulePreload: { polyfill: false },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': backend,
      '/media': backend,
    },
  },
})
