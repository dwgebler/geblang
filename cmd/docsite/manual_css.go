package main

const manualCSSFile = "manual.css"

const manualDarkTokens = `
  color-scheme: dark;
  --m-bg: #0d141b; --m-surface: #141e27; --m-text: #e4eaef; --m-muted: #9aa7b3; --m-toc: #b8c3cd;
  --m-border: #23303c; --m-border-soft: #1c2732; --m-hover: #1a2530;
  --m-link: #6ea8fe; --m-link-hover: #8bb9fe; --m-code: #ff8a95;
  --m-quote-bg: #10203a; --m-quote-text: #c9d6ea;
`

const manualCSS = `:root {
  color-scheme: light;
  --m-bg: #f7f8fa; --m-surface: #fff; --m-text: #212529; --m-muted: #6c757d; --m-toc: #495057;
  --m-border: #dee2e6; --m-border-soft: #edf0f2; --m-hover: #f8f9fa;
  --m-link: #0d6efd; --m-link-hover: #0a58ca; --m-code: #b02a37;
  --m-quote-bg: #f1f6ff; --m-quote-text: #24344d;
}
@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) {` + manualDarkTokens + `}
}
:root[data-theme="dark"] {` + manualDarkTokens + `}
*, *::before, *::after { box-sizing: border-box; }
html { -webkit-text-size-adjust: 100%; }
body {
  margin: 0;
  font-family: system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  font-size: 1rem;
  line-height: 1.5;
  color: var(--m-text);
  background: var(--m-bg);
}
h1, h2, h3, h4, h5, h6 { margin: 0 0 .5rem; font-weight: 500; line-height: 1.2; }
h1 { font-size: calc(1.375rem + 1.5vw); }
h2 { font-size: calc(1.325rem + .9vw); }
h3 { font-size: calc(1.3rem + .6vw); }
h4 { font-size: calc(1.275rem + .3vw); }
h5 { font-size: 1.25rem; }
h6 { font-size: 1rem; }
@media (min-width: 1200px) {
  h1 { font-size: 2.5rem; }
  h2 { font-size: 2rem; }
  h3 { font-size: 1.75rem; }
  h4 { font-size: 1.5rem; }
}
p, ul, ol, dl, pre, table, blockquote { margin-top: 0; margin-bottom: 1rem; }
ul, ol { padding-left: 2rem; }
ul ul, ol ol, ul ol, ol ul { margin-bottom: 0; }
dt { font-weight: 700; }
dd { margin-left: 0; margin-bottom: .5rem; }
hr { margin: 1rem 0; border: 0; border-top: 1px solid var(--m-border); }
a { color: var(--m-link); }
a:hover { color: var(--m-link-hover); }
img { max-width: 100%; height: auto; }
code, pre, kbd { font-family: SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace; font-size: .875em; }
code { color: var(--m-code); overflow-wrap: break-word; }
pre { display: block; background: #15171a; color: #f8f9fa; padding: 1rem; border-radius: .5rem; overflow: auto; }
pre code { color: inherit; font-size: inherit; overflow-wrap: normal; }
pre code.language-gb, pre code.language-geblang { color: #d6deeb; }
.code-wrap { position: relative; margin-bottom: 1rem; }
.code-wrap > pre { margin-bottom: 0; padding-right: 4.5rem; }
.copy-btn { position: absolute; top: .4rem; right: .5rem; background: #15171a; color: #9aa5b1; border: 1px solid #3a4048; border-radius: 4px; padding: .1rem .5rem; font: .72rem/1.5 system-ui, -apple-system, "Segoe UI", sans-serif; cursor: pointer; }
.copy-btn:hover, .copy-btn:focus-visible { color: #f8f9fa; border-color: #9aa5b1; }
@media print { .copy-btn { display: none; } }
blockquote { margin: 1.25rem 0; padding: 1rem 1.25rem; border-left: .35rem solid var(--m-link); background: var(--m-quote-bg); color: var(--m-quote-text); border-radius: 0 .5rem .5rem 0; }
blockquote > :last-child { margin-bottom: 0; }

.container-fluid { width: 100%; padding: 0 .75rem; margin: 0 auto; }
.manual-shell { max-width: 1240px; }
.row { display: flex; flex-wrap: wrap; margin: 0 -.75rem; }
.row > * { flex-shrink: 0; width: 100%; max-width: 100%; padding: 0 .75rem; }
.row.g-4 { row-gap: 1.5rem; }
@media (min-width: 576px) {
  .col-sm-2 { flex: 0 0 auto; width: 16.6667%; }
  .col-sm-10 { flex: 0 0 auto; width: 83.3333%; }
}
@media (min-width: 992px) {
  .col-lg-2 { flex: 0 0 auto; width: 16.6667%; }
  .col-lg-3 { flex: 0 0 auto; width: 25%; }
  .col-lg-7 { flex: 0 0 auto; width: 58.3333%; }
  .col-lg-9 { flex: 0 0 auto; width: 75%; }
  .d-lg-block { display: block !important; }
  .p-lg-5 { padding: 3rem !important; }
}

.d-none { display: none !important; }
.d-flex { display: flex !important; }
@media (min-width: 1200px) { .d-xl-inline { display: inline !important; } }
.justify-content-between { justify-content: space-between !important; }
.ms-auto { margin-left: auto !important; }
.me-2 { margin-right: .5rem !important; }
.me-3 { margin-right: 1rem !important; }
.mb-4 { margin-bottom: 1.5rem !important; }
.mt-5 { margin-top: 3rem !important; }
.pt-4 { padding-top: 1.5rem !important; }
.py-4 { padding-top: 1.5rem !important; padding-bottom: 1.5rem !important; }
.p-4 { padding: 1.5rem; }
.fw-semibold { font-weight: 600 !important; }
.small { font-size: .875em; }
.text-muted { color: var(--m-muted) !important; }
.lead { font-size: 1.25rem; font-weight: 300; }
.shadow-sm { box-shadow: 0 .125rem .25rem rgba(0, 0, 0, .075); }
.border-top { border-top: 1px solid var(--m-border) !important; }
.bg-dark { background-color: #212529 !important; }

.navbar { padding: .5rem 0; border-bottom: 1px solid rgba(255, 255, 255, .12); }
.navbar > .container-fluid { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: .5rem; }
.navbar-brand { margin-right: 1rem; padding: .3125rem 0; font-size: 1.25rem; color: #fff; text-decoration: none; white-space: nowrap; }
.navbar-brand:hover { color: #fff; }
.navbar-text { color: rgba(255, 255, 255, .55); }

.list-group { display: flex; flex-direction: column; padding: 0; margin: 0; border-radius: .375rem; overflow: hidden; }
.list-group-item { display: block; padding: .5rem 1rem; color: var(--m-text); background: var(--m-surface); border: 1px solid var(--m-border); text-decoration: none; }
.list-group-item + .list-group-item { border-top-width: 0; }
.list-group-item-action:hover { color: var(--m-text); background: var(--m-hover); }
.list-group-item.active { color: #fff; background: #0d6efd; border-color: #0d6efd; }
.list-group-item.nav-depth-1 { padding-left: 1.75rem; font-size: .94rem; }
.list-group-item.nav-depth-2 { padding-left: 2.5rem; font-size: .9rem; }
.list-group-item.nav-depth-3 { padding-left: 3.25rem; font-size: .88rem; }

.btn { display: inline-block; padding: .375rem .75rem; font-size: 1rem; line-height: 1.5; text-align: center; text-decoration: none; border: 1px solid transparent; border-radius: .375rem; cursor: pointer; background: transparent; }
.btn-sm { padding: .25rem .5rem; font-size: .875rem; border-radius: .25rem; }
.btn-primary { color: #fff; background: #0d6efd; border-color: #0d6efd; }
.btn-primary:hover { color: #fff; background: #0b5ed7; border-color: #0a58ca; }
.btn-outline-secondary { color: var(--m-muted); border-color: var(--m-muted); }
.btn-outline-secondary:hover { color: #fff; background: #6c757d; }
.btn-outline-light { color: #f8f9fa; border-color: #f8f9fa; }
.btn-outline-light:hover { color: #212529; background: #f8f9fa; }
.form-control { display: block; width: 100%; padding: .375rem .75rem; font-size: 1rem; line-height: 1.5; color: var(--m-text); background: var(--m-surface); border: 1px solid var(--m-border); border-radius: .375rem; }
.form-control-sm { padding: .25rem .5rem; font-size: .875rem; border-radius: .25rem; }

.table { width: 100%; border-collapse: collapse; vertical-align: top; }
.table th, .table td { padding: .5rem; border-bottom: 1px solid var(--m-border); text-align: left; }
.table-sm th, .table-sm td { padding: .25rem; }
.table-bordered th, .table-bordered td { border: 1px solid var(--m-border); }
.table-responsive { overflow-x: auto; -webkit-overflow-scrolling: touch; }

.sidebar { position: sticky; top: 5rem; max-height: calc(100vh - 6rem); overflow: auto; }
.content { background: var(--m-surface); border: 1px solid var(--m-border); border-radius: .5rem; }
.content h1, .content h2, .content h3 { scroll-margin-top: 5rem; }
.content h1 { margin-bottom: 1rem; }
.content h2 { margin-top: 2rem; padding-top: .5rem; border-top: 1px solid var(--m-border-soft); }
.toc-wrap { position: sticky; top: 5rem; max-height: calc(100vh - 6rem); overflow: auto; }
.toc-title { font-size: .78rem; text-transform: uppercase; letter-spacing: .04em; color: var(--m-muted); font-weight: 600; margin-bottom: .5rem; }
.toc-list { list-style: none; padding-left: 0; margin: 0; border-left: 1px solid var(--m-border); }
.toc-item a { display: block; padding: .2rem 0 .2rem .75rem; font-size: .85rem; color: var(--m-toc); text-decoration: none; border-left: 2px solid transparent; margin-left: -1px; }
.toc-item a:hover { color: var(--m-link); border-left-color: var(--m-link); }
.toc-item.toc-sub a { padding-left: 1.5rem; font-size: .82rem; color: var(--m-muted); }

.hl-comment { color: #6a9955; font-style: italic; }
.hl-string { color: #ecc48d; }
.hl-keyword { color: #c792ea; }
.hl-declaration { color: #82aaff; }
.hl-type { color: #ffcb6b; }
.hl-number { color: #f78c6c; }
.hl-constant { color: #ff9cac; }
.hl-function { color: #addb67; }
.hl-decorator { color: #7fdbca; }
.hl-operator { color: #89ddff; }
.theme-toggle { margin-left: .5rem; padding: .25rem .45rem; display: inline-grid; place-items: center; color: rgba(255, 255, 255, .75); background: transparent; border: 1px solid rgba(255, 255, 255, .25); border-radius: .375rem; cursor: pointer; }
.theme-toggle svg { width: 16px; height: 16px; fill: none; stroke: currentColor; stroke-width: 1.8; stroke-linecap: round; }
.theme-toggle:hover { color: #fff; border-color: rgba(255, 255, 255, .6); }
`
