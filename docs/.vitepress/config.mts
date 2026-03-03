import { defineConfig } from "vitepress";

// https://vitepress.dev/reference/site-config
export default defineConfig({
  head: [
    ['link', { rel: 'icon', href: '/arrflix/favicon.svg' }],
  ],
  title: "Arrflix Docs",
  description: "Self-hosted media management platform",
  // https://vitepress.dev/guide/deploy#setting-a-public-base-path
  base: "/arrflix/",
  themeConfig: {
    // https://vitepress.dev/reference/default-theme-config
    nav: [{ text: "Home", link: "/" }],

    sidebar: [
      {
        text: "Introduction",
        collapsed: false,
        items: [
          { text: "What is Arrflix?", link: "/guide/introduction" },
          { text: "Getting Started", link: "/guide/getting-started" },
        ],
      },
    ],

    socialLinks: [
      { icon: "github", link: "https://github.com/kyleaupton/arrflix" },
    ],
  },
});
