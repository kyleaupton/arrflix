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
      {
        text: "Concepts",
        collapsed: false,
        items: [
          { text: "How Arrflix Works", link: "/guide/how-arrflix-works" },
          { text: "Libraries", link: "/guide/libraries" },
          { text: "Indexers", link: "/guide/indexers" },
          { text: "Downloaders", link: "/guide/downloaders" },
          { text: "Name Templates", link: "/guide/name-templates" },
          { text: "Policy Engine", link: "/guide/policy-engine" },
          { text: "Importing & Hardlinks", link: "/guide/importing-and-hardlinks" },
        ],
      },
      {
        text: "Project",
        collapsed: false,
        items: [
          { text: "Roadmap", link: "/guide/roadmap" },
        ],
      },
    ],

    socialLinks: [
      { icon: "github", link: "https://github.com/kyleaupton/arrflix" },
    ],
  },
});
