import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import { unified } from '@astrojs/markdown-remark';
import legacyLinks from './scripts/legacy-links.mjs';
import { SITE_ORIGIN, SITE_BASE } from './scripts/site-config.mjs';

export default defineConfig({
  site: SITE_ORIGIN,
  base: SITE_BASE,
  trailingSlash: 'always',
  output: 'static',
  markdown: { processor: unified({ remarkPlugins: [[legacyLinks, { base: SITE_BASE }]] }) },
  integrations: [starlight({
    // Wrap long code lines instead of introducing inaccessible horizontal scroll regions.
    // Expressive Code preserves the original text when copied.
    expressiveCode: { defaultProps: { wrap: true } },
    title: 'Ruxbase',
    description: 'PostgreSQL-native, self-hosted backend platform documentation.',
    // Source-derived, hash-verified vector identity; approved by delegated review.
    logo: { light: './src/assets/brand/ruxbase-horizontal-black.svg', dark: './src/assets/brand/ruxbase-horizontal-white.svg', alt: 'Ruxbase', replacesTitle: true },
    favicon: '/favicon-32.png',
    head: [
      { tag: 'link', attrs: { rel: 'icon', type: 'image/png', sizes: '16x16', href: SITE_BASE + 'favicon-16.png' } },
      { tag: 'meta', attrs: { property: 'og:image', content: SITE_ORIGIN + SITE_BASE + 'social-preview.png' } },
      { tag: 'meta', attrs: { property: 'og:image:alt', content: 'Ruxbase logo and wordmark' } },
      { tag: 'meta', attrs: { property: 'og:image:width', content: '2172' } },
      { tag: 'meta', attrs: { property: 'og:image:height', content: '724' } },
      { tag: 'meta', attrs: { name: 'twitter:card', content: 'summary_large_image' } },
    ],
    social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/AbdullahZaher/ruxbase' }],
    sidebar: [
      { label: 'Getting started', items: [{ autogenerate: { directory: 'getting-started' } }] },
      { label: 'SDKs & Integrations', items: [{ autogenerate: { directory: 'integrations' } }] },
      { label: 'Data', items: [{ autogenerate: { directory: 'data' } }] },
      { label: 'Authorization', items: [{ autogenerate: { directory: 'authorization' } }] },
      { label: 'Guides', items: [{ autogenerate: { directory: 'guides' } }] },
      { label: 'AI & MCP', items: [{ autogenerate: { directory: 'ai' } }] },
      { label: 'Operations', items: [{ autogenerate: { directory: 'operations' } }] },
      { label: 'Reference', items: [{ autogenerate: { directory: 'reference' } }] },
      { label: 'Migration', items: [{ autogenerate: { directory: 'migration' } }] },
    ],
    customCss: ['./src/assets/brand/tokens.css', './src/styles/theme.css'],
  })],
});
