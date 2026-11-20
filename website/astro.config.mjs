import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

export default defineConfig({
  site: 'https://abdullahzaher.github.io',
  base: '/ruxbase/',
  output: 'static',
  integrations: [starlight({
    title: 'Ruxbase',
    description: 'PostgreSQL-native, self-hosted backend platform documentation.',
    logo: { src: './src/assets/brand/ruxbase_logo.png', alt: '' },
    social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/AbdullahZaher/ruxbase' }],
    customCss: ['./src/assets/brand/tokens.css', './src/styles/theme.css'],
  })],
});
