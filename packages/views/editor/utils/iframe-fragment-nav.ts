// Шим навигации по фрагментам для песочницы HTML-вложений в iframe без
// allow-same-origin.
const FRAGMENT_NAV_SHIM = `<script>
(function(){
  document.addEventListener('click', function(e) {
    if (e.defaultPrevented) return;
    var t = e.target;
    if (!t || typeof t.closest !== 'function') return;
    var a = t.closest('a[href]');
    if (!a) return;
    var href = a.getAttribute('href');
    if (!href || href.charAt(0) !== '#' || href === '#') return;
    var id;
    try { id = decodeURIComponent(href.slice(1)); } catch (_) { return; }
    if (!id) return;
    var dest = document.getElementById(id);
    if (!dest && typeof CSS !== 'undefined' && CSS.escape) {
      dest = document.querySelector('a[name="' + CSS.escape(id) + '"]');
    }
    if (!dest) return;
    e.preventDefault();
    dest.scrollIntoView({ behavior: 'smooth', block: 'start' });
  });
})();
</script>`;

export function withFragmentNavShim(html: string | undefined): string {
  return (html ?? '') + FRAGMENT_NAV_SHIM;
}

export const __FRAGMENT_NAV_SHIM__ = FRAGMENT_NAV_SHIM;
