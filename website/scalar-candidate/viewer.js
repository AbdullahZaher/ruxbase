const specURL = new URL('../openapi.json', location.href);
const config = {
  url: specURL.href, hideTestRequestButton: true, hideClientButton: true,
  persistAuth: false, telemetry: false, withDefaultFonts: false,
  agent: { disabled: true }, mcp: { disabled: true }, showDeveloperTools: 'never',
  customFetch: async (input, init = {}) => {
    const url = new URL(typeof input === 'string' || input instanceof URL ? input : input.url, location.href);
    if (url.href !== specURL.href || (init.method && init.method !== 'GET')) throw new Error('Documentation viewer does not send API requests');
    return fetch(specURL, {credentials:'omit'});
  },
};
try {
  // The documented standalone IIFE API avoids CDN entrypoint imports.
  await new Promise((ok, fail) => {
    const script = document.createElement('script');
    script.src = './vendor/standalone.js';
    script.onload = ok;
    script.onerror = () => fail(Error('Scalar standalone bundle could not be loaded'));
    document.head.append(script);
  });
  if (!globalThis.Scalar?.createApiReference) throw new Error('Scalar standalone API unavailable');
  globalThis.Scalar.createApiReference('#api-reference', config);
} catch (error) {
  document.querySelector('#api-reference').textContent = 'Viewer initialization failed. Use the static API reference.';
  throw error;
}
