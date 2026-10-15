# HTML (`html`)

The `html` module parses real-world HTML and lets you query it with CSS
selectors. It uses a lenient HTML5 parser (the same parsing rules a browser
follows), so it copes with unclosed tags, missing `<html>`/`<body>` wrappers,
and the other irregularities of pages in the wild. Reach for `html` when you
are scraping or extracting data from a page; use the `xml` module only for
strict, well-formed XML.

```gb
import html;
import io;

let doc = html.parse(pageSource);
let heading = doc.selectFirst("h1").text();
for (link in doc.select("a[href]")) {
    io.println(link.attr("href"));
}
```

## Parsing

`html.parse(source: string): Node` parses a document and returns its root
`Node`. Parsing is lenient and never throws on malformed markup:

- A fragment is wrapped in an implied `<html><body>` tree, so
  `html.parse("<li>x</li>")` still has a `body` you can query.
- Unclosed and misnested tags are repaired the way a browser would.

The returned root reports its tag as `#document`. The document element and
`body` are reachable through selectors or traversal.

```gb
let doc = html.parse("<li>only</li>");
doc.tag();                          # "#document"
doc.selectFirst("body").children(); # [<html.Node li>]
```

## Nodes

Every element and the document root are `Node` values. A node carries these
methods:

| Method | Returns | Description |
|--------|---------|-------------|
| `select(selector)` | `list<Node>` | Every descendant element matching the CSS selector, in document order. |
| `selectFirst(selector)` | `?Node` | The first matching descendant, or `null` if none match. |
| `text()` | `string` | The concatenated text of this node and all its descendants. Whitespace is preserved as written. |
| `attr(name)` | `?string` | The value of the named attribute, or `null` if the node has no such attribute. |
| `attrs()` | `dict<string, string>` | All attributes of the node as a dict. |
| `tag()` | `string` | The element's lowercased tag name (`#document` for the root, `""` for non-elements). |
| `html()` | `string` | The node's inner HTML (its children serialized back to markup). |
| `children()` | `list<Node>` | The node's direct child elements. Text and comment nodes are skipped. |
| `parent()` | `?Node` | The parent node, or `null` for the root. |

```gb
let doc = html.parse("<article><h1 id=\"t\">Title</h1><p>Body <em>text</em>.</p></article>");

let h1 = doc.selectFirst("h1");
h1.text();            # "Title"
h1.tag();             # "h1"
h1.attr("id");        # "t"
h1.attr("class");     # null

let p = doc.selectFirst("p");
p.text();             # "Body text."   (descendant text is included)
p.html();             # "Body <em>text</em>."
p.parent().tag();     # "article"

doc.selectFirst("article").children().length();  # 2  (h1 and p)
```

`select` returns every match; `selectFirst` short-circuits to the first and
returns `null` when nothing matches, so guard it before use:

```gb
let nav = doc.selectFirst("nav");
if (nav != null) {
    io.println(nav.html());
}
```

## CSS selectors

`select` and `selectFirst` accept standard CSS selectors. The supported syntax
includes:

- Type, class, id, and universal: `div`, `.title`, `#main`, `*`.
- Attributes: `[href]` (present), `[type="text"]` (exact), and the operators
  `^=` (prefix), `$=` (suffix), `*=` (substring), `~=` (word), `|=` (prefix or
  prefix-with-hyphen).
- Combinators: descendant (`ul li`), child (`ul > li`), adjacent sibling
  (`h1 + p`), and general sibling (`h1 ~ p`).
- Pseudo-classes such as `:first-child`, `:last-child`, `:nth-child(n)`,
  `:not(...)`, and the other structural pseudo-classes.
- Grouping with `,`: `h1, h2, h3` matches any of them.

```gb
doc.select("ul > li:nth-child(2)");      # the second list item
doc.select("a[href^=\"https://\"]");      # external links
doc.select("p:not(.footnote)");           # paragraphs except footnotes
doc.select("h1, h2");                      # all top-level headings
```

An invalid selector throws a `RuntimeError` naming the offending selector, so a
typo fails loudly rather than silently matching nothing.

## Examples

Extract every link with its text:

```gb
import html;
import io;

let doc = html.parse(pageSource);
for (a in doc.select("a[href]")) {
    io.println(a.text() + " -> " + (a.attr("href") as string));
}
```

Pull rows out of a table:

```gb
let rows = [];
for (tr in doc.select("table.data tr")) {
    let cells = [];
    for (td in tr.select("td")) {
        cells.push(td.text());
    }
    rows.push(cells);
}
```

Read the article title a redirect resolved to (with the HTTP client):

```gb
import html;
import http;
import io;

let resp = http.get("https://en.wikipedia.org/wiki/Special:Random");
let title = html.parse(resp.text()).selectFirst("h1").text();
io.println(title + " (" + resp.url() + ")");
```

## Notes

- `text()` walks the whole subtree, so it returns the visible text of nested
  elements too; it does not collapse or trim whitespace.
- `html()` is the node's inner HTML. To recover a child's own markup, select
  the child and call `html()` on it.
- `children()` yields element children only. To reach text content, use
  `text()`.
- Nodes are ordinary garbage-collected values. Holding any node keeps its
  document tree alive (parents and siblings remain reachable); once you drop
  all references to a parsed document, it is collected like any other value.
