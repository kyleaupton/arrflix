import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { VitePWA } from 'vite-plugin-pwa'

const api = {
  target: 'http://localhost:8080',
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    vue(),
    tailwindcss(),
    VitePWA({
      // injectManifest: we own the service worker (src/sw.ts) so it can handle
      // Web Push + notificationclick alongside Workbox precaching. The plugin
      // injects the precache manifest into self.__WB_MANIFEST at build time.
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.ts',
      // Prompt to update: a new build's worker installs and parks in "waiting"
      // rather than taking over silently. The app surfaces a "new version" toast
      // and only reloads when the user accepts — sw.ts skips waiting on the
      // SKIP_WAITING message. Icons come from pwa-assets.config.ts.
      registerType: 'prompt',
      pwaAssets: { config: true, overrideManifestIcons: true },
      manifest: {
        name: 'Arrflix',
        short_name: 'Arrflix',
        description: 'Self-hosted movie & series library',
        theme_color: '#0b0b0f',
        background_color: '#0b0b0f',
        display: 'standalone',
        start_url: '/',
      },
      injectManifest: {
        // Precache the app shell for instant repeat loads. The API is never
        // cached or shadowed — the SW's navigation fallback stops at /api so
        // requests pass through to the network as normal.
        globPatterns: ['**/*.{js,css,html,svg,png,ico,woff2}'],
      },
      // Enable the SW in dev so push can be exercised over http://localhost (a
      // secure context). type:'module' matches the sw.ts ESM source.
      devOptions: {
        enabled: true,
        type: 'module',
        navigateFallback: 'index.html',
      },
    }),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    proxy: {
      '/api': api,
    },
  },
})
