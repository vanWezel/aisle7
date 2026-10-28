import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// `npm run dev` proxies the API to a locally running `aisle7 serve`.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: { host: true, proxy: { '/api': 'http://localhost:8080' } },
})
