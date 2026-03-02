/// <reference types="vite/client" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}

export {}

declare module 'vue-router' {
  interface RouteMeta {
    public?: boolean
    layout?: 'immersive' | 'auth'
    setup?: boolean
  }
}
