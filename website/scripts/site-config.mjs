// Single current hosting target. A custom domain migration must update source links and tests.
export const SITE_ORIGIN = 'https://abdullahzaher.github.io';
export const SITE_BASE = '/ruxbase/';
export const absolute = (path) => new URL(path, SITE_ORIGIN).href;
export function routeFor(target) {
  if (target === 'index.md') return SITE_BASE;
  return SITE_BASE + target.replace(/\.md$/, '/');
}
export const markdownFor = (target) => SITE_BASE + target;
