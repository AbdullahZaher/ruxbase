const target = document.querySelector('#api-reference');
const specURL = new URL('../openapi.json', location.href);
const response = await fetch(specURL, { credentials:'omit' });
if (!response.ok) throw new Error('OpenAPI unavailable');
const spec = await response.json();
const config = {
  content: spec, hideTestRequestButton: true, hideClientButton: true,
  persistAuth: false, telemetry: false, withDefaultFonts: false,
  customFetch: async (input, init = {}) => {
    const url = new URL(typeof input === 'string' || input instanceof URL ? input : input.url, location.href);
    if (url.href !== specURL.href || (init.method && init.method !== 'GET')) throw new Error('Documentation viewer does not send API requests');
    return fetch(specURL, {credentials:'omit'});
  },
};
const esm = await fetch('./vendor/standalone.esm.js', {method:'HEAD'});
if (esm.ok) {
  const { createApiReference } = await import('./vendor/standalone.esm.js');
  createApiReference(target, config);
} else {
  throw new Error('Scalar ESM bundle unavailable');
}
