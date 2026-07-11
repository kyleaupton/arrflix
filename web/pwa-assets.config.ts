import { defineConfig, minimal2023Preset } from '@vite-pwa/assets-generator/config'

// Source → the full icon set. minimal2023Preset emits transparent 64/192/512,
// a padded maskable-512, apple-touch-180, and favicon.ico. The source
// (public/pwa-icon.svg) already bakes in the #0b0b0f ground; matching the
// maskable/apple padding fill to it keeps the safe-zone surround seamless
// rather than defaulting to a light ring.
const ground = '#0b0b0f'

export default defineConfig({
  headLinkOptions: { preset: '2023' },
  preset: {
    ...minimal2023Preset,
    maskable: { ...minimal2023Preset.maskable, resizeOptions: { background: ground } },
    apple: { ...minimal2023Preset.apple, resizeOptions: { background: ground } },
  },
  images: ['public/pwa-icon.svg'],
})
