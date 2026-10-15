package main

import (
	"encoding/json"
	"html"
	"path"
	"regexp"
	"strings"
)

const (
	searchIndexFile  = "search-index.js"
	searchScriptFile = "search.js"
	searchTextLimit  = 4000
)

var (
	sectionHeadingPattern = regexp.MustCompile(`(?s)<h([1-3]) id="([^"]+)">(.*?)</h[1-3]>`)
	spacePattern          = regexp.MustCompile(`\s+`)
)

type searchEntry struct {
	Title   string `json:"t"`
	Heading string `json:"h"`
	URL     string `json:"u"`
	Text    string `json:"x"`
}

// buildSearchIndex splits each rendered page at its h1-h3 headings so results link to the matching section.
func buildSearchIndex(pages []page) []searchEntry {
	var entries []searchEntry
	for _, p := range pages {
		if strings.EqualFold(path.Base(p.Output), "AGENTS.html") {
			continue
		}
		body := p.HTML
		matches := sectionHeadingPattern.FindAllStringSubmatchIndex(body, -1)
		add := func(heading, anchor, content string) {
			text := plainText(content)
			heading = plainText(heading)
			if text == "" && heading == "" {
				return
			}
			if len(text) > searchTextLimit {
				text = text[:searchTextLimit]
			}
			url := p.Output
			if anchor != "" {
				url += "#" + anchor
			}
			entries = append(entries, searchEntry{Title: p.Title, Heading: heading, URL: url, Text: text})
		}
		if len(matches) == 0 {
			add(p.Title, "", body)
			continue
		}
		if lead := body[:matches[0][0]]; strings.TrimSpace(plainText(lead)) != "" {
			add(p.Title, "", lead)
		}
		for i, m := range matches {
			end := len(body)
			if i+1 < len(matches) {
				end = matches[i+1][0]
			}
			add(body[m[6]:m[7]], body[m[4]:m[5]], body[m[1]:end])
		}
	}
	return entries
}

func plainText(fragment string) string {
	text := html.UnescapeString(tagPattern.ReplaceAllString(fragment, " "))
	return strings.TrimSpace(spacePattern.ReplaceAllString(text, " "))
}

// searchIndexJS wraps the index in a script so the manual can search it when opened from disk.
func searchIndexJS(entries []searchEntry) (string, error) {
	data, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	return "window.__docsearch=" + string(data) + ";\n", nil
}

func searchBoxHTML(current page) string {
	root := strings.TrimSuffix(relHref(current.Output, "index.html"), "index.html")
	return `
      <div class="docsearch" data-root="` + root + `">
        <input class="docsearch-input" type="search" placeholder="Search this manual" aria-label="Search this manual" autocomplete="off" spellcheck="false">
        <div class="docsearch-results" role="listbox" hidden></div>
      </div>
      <script src="` + root + searchScriptFile + `" defer></script>`
}

const searchScript = `(function () {
  var box = document.querySelector(".docsearch");
  if (!box) { return; }
  var root = box.getAttribute("data-root") || "";
  var input = box.querySelector(".docsearch-input");
  var panel = box.querySelector(".docsearch-results");
  var index = null, loading = false, active = -1, timer = null;

  function load(done) {
    if (index) { done(); return; }
    if (loading) { return; }
    loading = true;
    var script = document.createElement("script");
    script.src = root + "search-index.js";
    script.onload = function () {
      index = (window.__docsearch || []).map(function (e) {
        return { t: e.t, h: e.h, u: e.u, x: e.x, lt: e.t.toLowerCase(), lh: e.h.toLowerCase(), lx: e.x.toLowerCase() };
      });
      done();
    };
    document.head.appendChild(script);
  }

  function esc(text) {
    return text.replace(/[&<>"']/g, function (ch) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", "\"": "&quot;", "'": "&#39;" }[ch];
    });
  }

  function highlight(text, terms) {
    var out = esc(text);
    terms.forEach(function (term) {
      var pattern = new RegExp("(" + esc(term).replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + ")", "gi");
      out = out.replace(pattern, "<mark>$1</mark>");
    });
    return out;
  }

  function search(query) {
    var terms = query.toLowerCase().split(/\s+/).filter(Boolean);
    if (!terms.length) { return { terms: terms, hits: [] }; }
    var hits = [];
    index.forEach(function (e) {
      var score = 0;
      for (var i = 0; i < terms.length; i++) {
        var term = terms[i], s = 0;
        if (e.lh.indexOf(term) >= 0) { s += 10; }
        if (e.lt.indexOf(term) >= 0) { s += 4; }
        if (e.lx.indexOf(term) >= 0) { s += 2; }
        if (!s) { return; }
        score += s;
      }
      if (e.lh === query.toLowerCase().trim()) { score += 20; }
      hits.push({ e: e, score: score });
    });
    hits.sort(function (a, b) { return b.score - a.score; });
    return { terms: terms, hits: hits.slice(0, 12) };
  }

  function snippet(e, terms) {
    var at = -1;
    for (var i = 0; i < terms.length && at < 0; i++) { at = e.lx.indexOf(terms[i]); }
    if (at < 0) { at = 0; }
    var start = Math.max(0, at - 60);
    var text = e.x.slice(start, start + 170);
    return (start > 0 ? "... " : "") + highlight(text, terms) + (start + 170 < e.x.length ? " ..." : "");
  }

  function render(result) {
    active = -1;
    if (!input.value.trim()) { panel.hidden = true; return; }
    if (!result.hits.length) {
      panel.innerHTML = '<div class="docsearch-empty">No results</div>';
      panel.hidden = false;
      return;
    }
    panel.innerHTML = result.hits.map(function (hit, i) {
      var e = hit.e;
      var heading = e.h && e.h !== e.t ? highlight(e.h, result.terms) + '<span class="docsearch-page">' + esc(e.t) + "</span>" : highlight(e.t, result.terms);
      return '<a class="docsearch-item" role="option" id="docsearch-' + i + '" href="' + esc(root + e.u) + '"><span class="docsearch-heading">' + heading + '</span><span class="docsearch-snippet">' + snippet(e, result.terms) + "</span></a>";
    }).join("");
    panel.hidden = false;
  }

  function move(step) {
    var items = panel.querySelectorAll(".docsearch-item");
    if (!items.length) { return; }
    if (active >= 0) { items[active].classList.remove("active"); }
    active = (active + step + items.length) % items.length;
    items[active].classList.add("active");
    items[active].scrollIntoView({ block: "nearest" });
  }

  input.addEventListener("focus", function () { load(function () { if (input.value) { render(search(input.value)); } }); });
  input.addEventListener("input", function () {
    clearTimeout(timer);
    timer = setTimeout(function () { load(function () { render(search(input.value)); }); }, 80);
  });
  input.addEventListener("keydown", function (event) {
    if (event.key === "ArrowDown") { move(1); event.preventDefault(); }
    else if (event.key === "ArrowUp") { move(-1); event.preventDefault(); }
    else if (event.key === "Escape") { panel.hidden = true; input.blur(); }
    else if (event.key === "Enter") {
      var items = panel.querySelectorAll(".docsearch-item");
      var target = items[active >= 0 ? active : 0];
      if (target) { window.location.href = target.getAttribute("href"); }
    }
  });
  document.addEventListener("keydown", function (event) {
    if (event.key === "/" && document.activeElement !== input && !/input|textarea/i.test(document.activeElement.tagName)) {
      input.focus();
      event.preventDefault();
    }
  });
  document.addEventListener("click", function (event) {
    if (!box.contains(event.target)) { panel.hidden = true; }
  });
})();
`

const searchCSS = `
.docsearch { position: relative; margin-left: auto; margin-right: 1rem; width: min(22rem, 100%); }
.docsearch-input { width: 100%; padding: .3rem .6rem; font-size: .875rem; border: 1px solid #495057; border-radius: .375rem; background: #343a40; color: #f8f9fa; }
.docsearch-input::placeholder { color: #adb5bd; }
.docsearch-input:focus { outline: none; border-color: #86b7fe; background: #fff; color: #212529; }
.docsearch-results { position: absolute; z-index: 1000; top: calc(100% + .35rem); right: 0; width: min(32rem, 90vw); max-height: 70vh; overflow-y: auto; background: #fff; border: 1px solid #dee2e6; border-radius: .5rem; box-shadow: 0 .5rem 1.5rem rgba(0,0,0,.18); }
.docsearch-item { display: block; padding: .55rem .8rem; border-bottom: 1px solid #f1f3f5; color: #212529; text-decoration: none; }
.docsearch-item:last-child { border-bottom: 0; }
.docsearch-item:hover, .docsearch-item.active { background: #eef4ff; }
.docsearch-heading { display: block; font-weight: 600; font-size: .9rem; }
.docsearch-page { margin-left: .5rem; font-weight: 400; font-size: .75rem; color: #6c757d; }
.docsearch-snippet { display: block; margin-top: .15rem; font-size: .8rem; color: #495057; }
.docsearch-item mark { padding: 0; background: #fff3bf; }
.docsearch-empty { padding: .75rem .8rem; font-size: .875rem; color: #6c757d; }
@media (max-width: 991.98px) { .docsearch { width: 100%; margin: .5rem 0 0; } .docsearch-results { left: 0; right: auto; width: 100%; } }
`
