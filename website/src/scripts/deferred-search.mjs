// Pagefind's generated local UI is loaded only after opening the native dialog.
// No dependency, external request, idle preload or hand-edited generated file.
function loadAsset(url, kind) {
  return new Promise((resolve, reject) => {
    const element = document.createElement(kind === 'style' ? 'link' : 'script');
    if (kind === 'style') { element.rel = 'stylesheet'; element.href = url; }
    else { element.src = url; element.async = true; }
    const timer = setTimeout(() => finish(new Error('Search asset timed out')), 15000);
    function finish(error) {
      clearTimeout(timer);
      element.onload = element.onerror = null;
      if (error) { element.remove(); reject(error); }
      else resolve(element);
    }
    element.onload = () => finish();
    element.onerror = () => finish(new Error('Search asset failed'));
    document.head.append(element);
  });
}

class DeferredSearch extends HTMLElement {
  connectedCallback() {
    if (this.handlers) return;
    this.handlers = new AbortController();
    const signal = this.handlers.signal;
    const opener = this.querySelector('[data-open-modal]');
    const close = this.querySelector('[data-close-modal]');
    const retry = this.querySelector('[data-search-retry]');
    const dialog = this.querySelector('dialog');
    const status = this.querySelector('[data-search-status]');
    const target = this.querySelector('#starlight__search');
    const base = this.dataset.base;
    if (!base || !base.startsWith('/') || !base.endsWith('/')) return;

    const focusInput = () => { if (dialog.open) target.querySelector('input')?.focus(); };
    const initialize = () => {
      if (this.ui) { focusInput(); return Promise.resolve(); }
      if (this.loading) return this.loading;
      status.hidden = false;
      status.textContent = 'Loading search…';
      retry.hidden = true;
      this.loading = (async () => {
        const items = await Promise.allSettled([
          this.css ? Promise.resolve(this.css) : loadAsset(base + 'pagefind/pagefind-ui.css', 'style'),
          typeof window.PagefindUI === 'function' ? Promise.resolve(null) : loadAsset(base + 'pagefind/pagefind-ui.js', 'script'),
        ]);
        if (items[0].status === 'fulfilled') this.css = items[0].value;
        if (items.some(item => item.status === 'rejected') || typeof window.PagefindUI !== 'function') throw new Error('Search unavailable');
        this.ui = new window.PagefindUI({
          element: target, baseUrl: base, bundlePath: base + 'pagefind/',
          showImages: false, showSubResults: true,
          ranking: { pageLength: .1, termFrequency: .1, termSaturation: 2, termSimilarity: 9, diacriticSimilarity: .8 },
        });
        status.hidden = true;
        focusInput();
      })().catch(() => {
        status.textContent = 'Search could not load. Please retry.';
        retry.hidden = false;
        if (dialog.open) retry.focus();
      }).finally(() => { this.loading = null; });
      return this.loading;
    };
    const open = () => {
      if (!dialog.open) dialog.showModal();
      document.body.toggleAttribute('data-search-modal-open', true);
      void initialize();
    };
    const dismiss = () => { if (dialog.open) dialog.close(); };
    opener.disabled = false;
    if (/(Mac|iPhone|iPad)/i.test(navigator.platform)) {
      opener.setAttribute('aria-keyshortcuts', 'Meta+K');
      opener.querySelector('kbd').textContent = '⌘ K';
    }
    opener.addEventListener('click', open, { signal });
    close.addEventListener('click', dismiss, { signal });
    retry.addEventListener('click', () => void initialize(), { signal });
    dialog.addEventListener('click', event => {
      if (event.target === dialog || event.target.closest?.('a[href]')) dismiss();
    }, { signal });
    dialog.addEventListener('close', () => {
      document.body.removeAttribute('data-search-modal-open');
      opener.focus();
    }, { signal });
    window.addEventListener('keydown', event => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        if (dialog.open) dismiss(); else open();
      }
    }, { signal });
  }
  disconnectedCallback() {
    this.handlers?.abort();
    this.handlers = null;
    document.body.removeAttribute('data-search-modal-open');
  }
}
if (!customElements.get('site-search')) customElements.define('site-search', DeferredSearch);
