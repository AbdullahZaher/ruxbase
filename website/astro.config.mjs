import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import legacyLinks from './scripts/legacy-links.mjs';

export default defineConfig({
  site: 'https://abdullahzaher.github.io',
  base: '/ruxbase/',
  output: 'static',
  markdown: { remarkPlugins: [[legacyLinks, { base: '/ruxbase/' }]] },
  integrations: [starlight({
    title: 'Ruxbase',
    description: 'PostgreSQL-native, self-hosted backend platform documentation.',
    // Keep the original PNG in the header until vector candidates are reviewed.
    logo: { src: './src/assets/brand/ruxbase_logo.png', alt: '' },
    favicon: '/favicon.svg',
    social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/AbdullahZaher/ruxbase' }],
    sidebar: [
      { label: 'Getting started', autogenerate: { directory: 'getting-started' } },
      { label: 'Data', autogenerate: { directory: 'data' } },
      { label: 'Authorization', autogenerate: { directory: 'authorization' } },
      { label: 'Guides', autogenerate: { directory: 'guides' } },
      { label: 'AI & MCP', autogenerate: { directory: 'ai' } },
      { label: 'Operations', autogenerate: { directory: 'operations' } },
      { label: 'Migration', autogenerate: { directory: 'migration' } },
    ],
    customCss: ['./src/assets/brand/tokens.css', './src/styles/theme.css'],
  })],
});
