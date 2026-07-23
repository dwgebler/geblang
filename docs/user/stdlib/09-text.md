# Text, Regex, Markdown, And Templates

## String Methods

Strings are immutable values. Every method returns a new string or a derived
value - the original is unchanged.

### Inspection

| Method | Returns | Description |
|--------|---------|-------------|
| `length()` | `int` | Number of Unicode code points |
| `isEmpty()` | `bool` | `true` when the string has no characters |
| `isBlank()` | `bool` | `true` when empty or only whitespace |
| `get(index)` | `string` | Single character at `index` (negative = from end) |
| `chars()` | `list<string>` | All characters as a list |
| `codePointAt(index)` | `int` | Unicode code point at `index`, or `null` if out of range (the "ord" of one character) |
| `codePoints()` | `list<int>` | All Unicode code points as a list |
| `graphemes()` | `list<string>` | Grapheme clusters (user-perceived characters) |
| `graphemeLength()` | `int` | Number of grapheme clusters |
| `truncateGraphemes(n)` | `string` | First `n` grapheme clusters |

```gb
import io;

let s = "hello";
io.println(s.length());     # 5
io.println(s.isEmpty());    # false
io.println(s.get(0));       # h
io.println(s.get(-1));      # o
io.println(s.chars());      # [h, e, l, l, o]
io.println(s.codePointAt(0)); # 104
```

### Graphemes vs code points

`length()`, `chars()`, and `codePoints()` work in Unicode **code points**.
A user-perceived character (a "grapheme cluster") can be several code points:
a base letter plus combining marks, or an emoji built from a ZWJ sequence.
Use the `graphemes` methods (UAX #29 segmentation) when you mean
what the reader sees, for example display width, truncation, or cursor steps.

Code-point indexing (`s[i]`), `substring` / `slice`, and `length()` cache rune
information for strings longer than 256 bytes, making repeated access
amortized O(1). Strings at or below 256 bytes are rescanned per call, limiting
each scan to at most 256 bytes.

```gb
import io;

let family = "\u{1F468}\u{200D}\u{1F469}\u{200D}\u{1F467}";  # man+ZWJ+woman+ZWJ+girl
io.println(family.length());          # 5  (code points)
io.println(family.graphemeLength());  # 1  (one perceived character)

let accented = "e\u{301}llo";          # e + combining acute = "éllo"
io.println(accented.length());         # 5
io.println(accented.graphemes());      # [é, l, l, o]

io.println("héllo wörld".truncateGraphemes(5));  # héllo
io.println("geblang".graphemes().reverse().join("")); # reverse by grapheme
```

### Searching

| Method | Returns | Description |
|--------|---------|-------------|
| `contains(needle)` | `bool` | `true` when `needle` appears anywhere in the string |
| `startsWith(prefix)` | `bool` | `true` when the string begins with `prefix` |
| `endsWith(suffix)` | `bool` | `true` when the string ends with `suffix` |
| `indexOf(needle)` | `int` | First index of `needle`, or `-1` if not found |
| `lastIndexOf(needle)` | `int` | Last index of `needle`, or `-1` if not found |
| `search(needle)` | `list<int>` | Every (rune) start position of `needle`, or every character index where the callable `needle` returns true |
| `searchPattern(regex)` | `list<int>` | Every match start position (rune index) for `regex` |
| `count(needle)` | `int` | Number of non-overlapping occurrences of `needle` |
| `equalsIgnoreCase(other)` | `bool` | Case-insensitive equality |
| `containsIgnoreCase(needle)` | `bool` | Case-insensitive substring test |

```gb
import io;

let s = "hello world";
io.println(s.contains("world"));   # true
io.println(s.startsWith("hello")); # true
io.println(s.endsWith("world"));   # true
io.println(s.indexOf("l"));        # 2
io.println(s.lastIndexOf("l"));    # 9
io.println(s.count("l"));          # 3
io.println(s.equalsIgnoreCase("HELLO WORLD"));   # true
io.println(s.containsIgnoreCase("WORLD"));       # true
```

### Slicing And Substrings

`substring(start[, end])` and `slice(start[, end])` are aliases - both extract a
sub-sequence by code-point index.  Negative indices count from the end.

| Method | Returns | Description |
|--------|---------|-------------|
| `substring(start[, end])` | `string` | Characters from `start` up to (not including) `end` |
| `slice(start[, end])` | `string` | Same as `substring` |

```gb
import io;

let s = "hello world";
io.println(s.substring(6));      # world
io.println(s.substring(0, 5));   # hello
io.println(s.slice(-5));         # world
io.println(s.slice(0, -6));      # hello
```

### Transformation

| Method | Returns | Description |
|--------|---------|-------------|
| `lower()` | `string` | All characters lower-cased |
| `upper()` | `string` | All characters upper-cased |
| `capitalize()` | `string` | First character upper-cased, the rest lower-cased |
| `title()` | `string` | Each whitespace-separated word title-cased |
| `trim()` | `string` | Leading and trailing whitespace removed |
| `trimStart()` | `string` | Leading whitespace removed |
| `trimEnd()` | `string` | Trailing whitespace removed |
| `replace(old, new[, n])` | `string` | Replace occurrences of `old` with `new`; `n` limits replacements |
| `reverse()` | `string` | Characters in reversed order |
| `repeat(n)` | `string` | String repeated `n` times |
| `padStart(len[, pad])` | `string` | Pad to at least `len` characters on the left |
| `padEnd(len[, pad])` | `string` | Pad to at least `len` characters on the right |
| `removePrefix(p)` | `string` | Strip prefix `p` if present, else unchanged |
| `removeSuffix(s)` | `string` | Strip suffix `s` if present, else unchanged |

```gb
import io;

let s = "  Hello, World!  ";
io.println(s.trim());                       # Hello, World!
io.println(s.lower());                      # "  hello, world!  "
io.println(s.upper());                      # "  HELLO, WORLD!  "
io.println("abc".repeat(3));               # abcabcabc
io.println("hello".reverse());             # olleh
io.println("7".padStart(4, "0"));          # 0007
io.println("hi".padEnd(5, "."));           # hi...
io.println("hello world".replace("o", "0")); # hell0 w0rld
io.println("hello world".replace("o", "0", 1)); # hell0 world
io.println("hELLO wORLD".capitalize());     # Hello world
io.println("hELLO wORLD".title());          # Hello World
io.println("/usr/bin".removePrefix("/"));   # usr/bin
io.println("report.txt".removeSuffix(".txt")); # report
```

### Splitting And Joining

| Method | Returns | Description |
|--------|---------|-------------|
| `split(sep)` | `list<string>` | Split on `sep`; returns list of parts |
| `lines()` | `list<string>` | Split on line boundaries (LF and CRLF); no trailing empty line |
| `format(...)` | `string` | `printf`-style formatting with positional `{}` placeholders |

```gb
import io;

let csv = "a,b,c,d";
let parts = csv.split(",");
io.println(parts);          # [a, b, c, d]
io.println(parts.length()); # 4

io.println("line1\nline2\nline3".lines()); # [line1, line2, line3]

let msg = "Hello, {}! You have {} messages.".format("Ada", 3);
io.println(msg);  # Hello, Ada! You have 3 messages.
```

### Conversion

| Method | Returns | Description |
|--------|---------|-------------|
| `toString()` | `string` | Returns the string itself (identity) |
| `isInt()` | `bool` | `true` exactly when `toInt()` would succeed (same parse: signs, `0b`/`0o`/`0x` bases, `_` separators) |
| `isDecimal()` | `bool` | `true` exactly when `toDecimal()` would succeed |
| `isNumeric()` | `bool` | `true` when the string parses as an int or a decimal |

These predicates never throw, so you can test a string before converting
instead of wrapping the cast in try/catch. They reuse the exact `toInt` /
`toDecimal` parse, so `s.isInt()` is `true` if and only if `s.toInt()`
does not raise.

Cast with `as int`, `as decimal`, `as float`, `as bool` where needed. Also new in 1.0.2: `as bytes` encodes the string as UTF-8, and a `bytes` value cast back `as string` decodes UTF-8 (the cast raises a catchable `RuntimeError` if the byte sequence is not valid UTF-8).

```gb
let b = "résumé" as bytes;
io.println(b.length);     # 8 (two two-byte runes plus four ASCII)
io.println(b as string);  # résumé
```

---

## String Factories: `string`

Import `string`. The module is a small namespace for static / factory functions that don't belong on a string instance (you can't ask a non-existent string for its codepoint). Everything else string-related is an instance method - see [String Methods](#string-methods) above.

| Function | Returns | Description |
|----------|---------|-------------|
| `fromCodePoint(n)` | `string` | Single-character string for the Unicode codepoint `n` (this is "chr"). Rejects negative values, values above U+10FFFF, and the UTF-16 surrogate range U+D800..U+DFFF. |
| `fromCodePoints(list<int>)` | `string` | Multi-character string built from a list of codepoints. Same validation per element. |
| `compare(a, b)` | `int` | Three-way comparison returning -1 / 0 / +1. Pass it straight to `xs.sort(string.compare)` (sort accepts a three-way comparator). Compares the underlying UTF-8 bytes, which agrees with codepoint order. |
| `equalsFold(a, b)` | `bool` | Case-insensitive equality respecting Unicode case folding. `string.equalsFold("CafÉ", "café")` is `true`. |

```gb
import string;
import io;

io.println(string.fromCodePoint(65));               # A
io.println(string.fromCodePoint(8364));             # €
io.println(string.fromCodePoints([72, 105, 33]));   # Hi!
io.println(string.compare("apple", "banana"));      # -1
io.println(string.equalsFold("Hello", "HELLO"));    # true
```

Geblang has no separate `chr` / `ord`: `string.fromCodePoint(n)` is
`chr` (codepoint to character) and `s.codePointAt(i)` is `ord`
(character to codepoint). `s.codePoints()` and `string.fromCodePoints`
convert a whole string to and from a `list<int>` of codepoints.

For *timing-attack-safe* string equality (HMAC verification, token comparison, etc.) use `secrets.constantTimeEqual(a, b)` from the security module - see [Security](12-security.md). `string.equalsFold` and `string.compare` are **not** constant-time.

---

### Regex string-method variants

Three convenience methods route through the `re` module without
requiring the `import re`:

| Method | Returns | Description |
|--------|---------|-------------|
| `splitRegex(pattern)` | `list<string>` | Split by a regex pattern. |
| `replaceRegex(pattern, replacement)` | `string` | Replace every regex match. `$1` / `$2` capture-group references work in the replacement. |
| `matchesRegex(pattern)` | `bool` | True when the string contains a match. |

```gb
let parts = "foo, bar; baz".splitRegex("[,;] *");          # ["foo","bar","baz"]
let normalised = "John Smith".replaceRegex("(\\w+) (\\w+)", "$2, $1"); # "Smith, John"
let ok = "foo123".matchesRegex("[a-z]+[0-9]+");            # true
```

The pattern compile cache (introduced in 1.0.5 for the `re` module)
applies here too, so repeated calls with the same pattern skip the
recompile.

## Builder: `strings.StringBuilder`

Import `strings`. `StringBuilder` is a builder-backed accumulator. Use it for tight loops that append many fragments - internally a single `strings.Builder` grows amortised O(n) instead of the O(n²) cost of repeated `acc = acc + fragment` allocating a fresh string every iteration.

```gb
import strings;
import io;

let sb = strings.StringBuilder();
for (int i = 0; i < 10; i++) {
    sb.append("part-");
    sb.append(i as string);
    sb.appendLine("");
}
io.println(sb.build());
sb.dispose();
```

| Method | Returns | Description |
|--------|---------|-------------|
| `StringBuilder(initial = "")` | `StringBuilder` | Construct a new builder, optionally pre-seeded with `initial`. |
| `append(s)` | `StringBuilder` | Append a fragment. Returns `this` for chaining. |
| `appendLine(s)` | `StringBuilder` | Append a fragment followed by `\n`. Returns `this`. |
| `build()` | `string` | Materialise the accumulated content. |
| `length()` | `int` | Current byte length. |
| `clear()` | `StringBuilder` | Reset the buffer to empty. Returns `this`. |
| `dispose()` | `void` | Release the underlying handle. Safe to call multiple times. Call in long-running processes to free the builder. |

For the common `acc = acc + "literal"` idiom inside a loop, the bytecode compiler **automatically** swaps the local to a builder-backed representation behind the scenes, then materialises it back to a string on the next read. No source change required:

```gb
string acc = "";
for (int i = 0; i < 10000; i++) {
    acc = acc + "x";          # compiler emits builder-backed append
}
io.println(acc.length());     # 10000 - acc materialises here
```

Reach for the explicit `StringBuilder` when the auto-rewrite doesn't apply: dynamic (non-literal) RHS, accumulator written through a class field, or when you want chained writes (`sb.append("a").append("b")`).

### Low-level primitives: `strbuilder`

`StringBuilder` is implemented in `stdlib/strings.gb` on top of the `strbuilder` native module. The handle-based primitives are available directly for advanced uses:

| Function | Returns | Description |
|----------|---------|-------------|
| `strbuilder.new(initial = "")` | handle | Create a new builder; returns an opaque handle. |
| `strbuilder.append(h, s)` | handle | Append `s` to the builder; returns `h`. |
| `strbuilder.appendLine(h, s)` | handle | Append `s` followed by `\n`. |
| `strbuilder.build(h)` | `string` | Materialise the current content. |
| `strbuilder.length(h)` | `int` | Current byte length. |
| `strbuilder.clear(h)` | handle | Reset the buffer. |
| `strbuilder.dispose(h)` | `null` | Release the handle. |

---

## Regex: `re`

Import `re`. The module is a thin wrapper over Go's [`regexp/syntax`](https://pkg.go.dev/regexp/syntax) (RE2 dialect, no backreferences but full Unicode, anchors, and lookahead-free alternation).

- `test(pattern, text)` - returns `bool`.
- `find(pattern, text)` - returns the first match as a `string`, or `null`.
- `findAll(pattern, text)` - returns every non-overlapping match as `list<string>`.
- `match(pattern, text)` - returns a dict with the first match plus capture groups (see below), or `null`.
- `matchAll(pattern, text)` - returns `list<dict>` with one entry per non-overlapping match.
- `replace(pattern, replacement, text)` - returns a `string`. Use `$1`, `$2`, `${name}` in `replacement` to reference capture groups.
- `split(pattern, text)` - returns a `list<string>`.
- `compile(pattern)` - validates the pattern eagerly and returns a
  reusable `Pattern` object.

### Compiled patterns

`re.compile(pattern)` returns a `Pattern` that carries the compiled
expression, so a loop states the pattern once and its methods drop
the pattern argument:

```gb
let id = re.compile("[a-z]+[0-9]+");
for (token in tokens) {
    if (id.test(token)) { ... }
}
```

`Pattern` has the same surface as the module functions without the
leading `pattern`: `test(text)`, `find(text)`, `findAll(text)`,
`match(text)`, `matchAll(text)`, `replace(replacement, text)`,
`split(text)`. Invalid patterns raise at `compile` time rather than
at first use. Performance is on par with the cached module functions
for a single hot pattern, and steadier when several patterns are
used in the same loop (each compiled form is retained, where the
plain functions share one most-recent-pattern cache slot).

### Match results

`re.match` and `re.matchAll` return dicts in the same shape:

| Field | Type | Description |
|-------|------|-------------|
| `text` | `string` | The whole match (alias for `groups[0]`). |
| `span` | `list<int>` | `[start, end]` position of the whole match (1.33.0). |
| `groups` | `list<string>` | Every group in order. `groups[0]` is the whole match; `groups[1]`, `groups[2]`, ... are the parenthesised subexpressions. |
| `spans` | `list` | `[start, end]` per group, aligned with `groups`; `null` for a group that did not participate in the match (1.33.0). |
| `named` | `dict<string, string>` | Named capture groups (`(?P<name>...)`) keyed by name. |
| `namedSpans` | `dict` | `[start, end]` per named group; `null` for an unmatched named group (1.33.0). |

Spans are character offsets (not bytes), end-exclusive, so they
compose with string indexing and slicing:
`text.substring(span[0], span[1])` is exactly the matched text, and
`span[1] - span[0]` is the match length. Multibyte characters count
as one position.

```gb
import re;
import io;

let m = re.match("(?P<word>[A-Za-z]+)([0-9]+)", "Ada123");
io.println(m["text"]);              # Ada123
io.println(m["span"]);              # [0, 6]
io.println(m["groups"][1]);         # Ada      (numbered group 1)
io.println(m["groups"][2]);         # 123      (numbered group 2)
io.println(m["spans"][2]);          # [3, 6]   (position of group 2)
io.println(m["named"]["word"]);     # Ada      (named group)
io.println(m["namedSpans"]["word"]); # [0, 3]

# Extract every name=value pair from a free-form string.
let pairs = re.matchAll("(?P<k>\\w+)=\"(?P<v>[^\"]*)\"",
                       "user=\"ada\" role=\"admin\"");
for (pair in pairs) {
    io.println(pair["named"]["k"] + " -> " + pair["named"]["v"]);
}

# Where did the pattern match?
let text = "v12 and 23";
for (hit in re.matchAll("\\d+", text)) {
    let s = hit["span"];
    io.println("${hit["text"]} at ${s[0]}-${s[1]}");   # 12 at 1-3, 23 at 8-10
}
```

### Anchors and flags

Geblang regexes follow Go's RE2 syntax. Anchors `^`/`$` match at start/end of
input by default; pass `(?m)` to make them match line boundaries. Other useful
inline flags:

- `(?i)` - case-insensitive
- `(?s)` - dot matches newline
- `(?U)` - swap greedy and non-greedy quantifiers

```gb
io.println(re.test("(?i)^hello",  "Hello World"));   # true
io.println(re.test("(?s)foo.bar", "foo\nbar"));      # true
```

---

## PCRE-compatible regex: `pcre`

Import `pcre`. `pcre` runs a PCRE-style engine (backed by .NET's
regex syntax) that supports the features RE2 omits: lookahead,
lookbehind, backreferences, atomic groups, possessive quantifiers,
and named captures via either `(?P<name>...)` (PHP / Python) or
`(?<name>...)` (.NET / PCRE2) syntax. Use it when porting PHP
code or when the pattern needs features RE2 can't express.

`re` and `pcre` coexist. Prefer `re` for hot paths or any input
that may be user-controlled (RE2 has linear-time matching and no
catastrophic backtracking); reach for `pcre` when you need the
richer syntax.

Every function accepts an optional flags string as the last
argument:

| Flag | Meaning |
|------|---------|
| `i` | Case-insensitive |
| `m` | Multiline (`^` / `$` match line boundaries) |
| `s` | Dotall (`.` matches newlines) |
| `x` | Extended (whitespace ignored, `#` comments allowed) |

### Functions

- `test(pattern, text, flags = "")` - returns `bool`.
- `find(pattern, text, flags = "")` - first match as a `string`, or `null`.
- `findAll(pattern, text, flags = "")` - every non-overlapping match as `list<string>`.
- `match(pattern, text, flags = "")` - dict with `text` / `span` / `groups` / `spans` / `named` / `namedSpans` (same shape as `re.match`), or `null`.
- `compile(pattern, flags = "")` - returns a reusable `Pattern` that
  carries the pattern and flags; its methods mirror the functions
  without the `pattern`/`flags` arguments (e.g.
  `pcre.compile("^foo$", "im").test(text)`).
- `matchAll(pattern, text, flags = "")` - `list<dict>`.
- `replace(pattern, replacement, text, flags = "")` - returns a `string`. Use `$1`, `$2`, `${name}` for backrefs.
- `split(pattern, text, flags = "")` - returns a `list<string>`.
- `quote(text)` - escapes regex metacharacters in a literal string.

### Examples

```gb
import pcre;
import io;

# Lookahead: PCRE-only.
io.println(pcre.find('\w+(?=ing\b)', "swimming and running"));  # swimm

# Lookbehind: PCRE-only.
io.println(pcre.find('(?<=\$)\d+', "price is $42"));            # 42

# Backreferences: PCRE-only.
io.println(pcre.test('(\w+)\s+\1', "hello hello"));             # true

# PHP-style (?P<name>...) syntax works unchanged.
let m = pcre.match('(?P<word>[a-z]+)(?P<num>\d+)', "abc123");
io.println(m["named"]["word"]);                                  # abc

# Numbered backreference in replacement.
io.println(pcre.replace('(\w+) (\w+)', "$2 $1", "hello world")); # world hello

# Case-insensitive via flags.
io.println(pcre.test("hello", "HELLO", "i"));                    # true

# Escape user input before splicing into a pattern.
let needle = pcre.quote("a.b+c");
io.println(pcre.test(needle, "x a.b+c y"));                      # true
```

---

## Markdown: `markdown`

Import `markdown`. The module supports full [GitHub Flavored Markdown](https://github.github.com/gfm/) (GFM) - tables, strikethrough, task lists, autolinks, ordered lists, blockquotes, horizontal rules, setext headings, and raw HTML passthrough.

- `renderHtml(source)` - render to HTML string.
- `parse(source)` - returns a `list<dict>` of block nodes. Each dict has a `"type"` key; additional keys depend on the type (see below).
- `stripText(source)` - extract all plain text, stripping markup.

Block types returned by `parse`:

| `type` | Additional keys |
|--------|----------------|
| `"heading"` | `level: int`, `text: string` |
| `"paragraph"` | `text: string` |
| `"list"` | `items: list<string>` |
| `"ordered_list"` | `items: list<string>` |
| `"task_list"` | `items: list<dict>` - each `{text: string, checked: bool}` |
| `"code"` | `lang: string`, `code: string` |
| `"table"` | `headers: list<string>`, `rows: list<list<string>>` |
| `"blockquote"` | `text: string` |
| `"hr"` | _(no extra keys)_ |
| `"html"` | `html: string` |

```gb
import markdown;
import io;

let src = "## Hello\n\n| col1 | col2 |\n|------|------|\n| a | b |\n\n- [x] done\n- [ ] todo";
io.println(markdown.renderHtml(src));

let blocks = markdown.parse(src);
io.println(blocks[0]["type"]);          # heading
io.println(blocks[1]["headers"][0]);    # col1
io.println(blocks[2]["items"][0]["checked"]);  # true
```

---

## Unicode normalisation: `unicode` (1.6.0)

The `unicode` module exposes the four Unicode normalisation
forms via `unicode.normalize(s, form)`. `form` is the canonical
SPDX-style name: `"NFC"`, `"NFD"`, `"NFKC"`, or `"NFKD"`.

```gb
import unicode;

let nfd = "é";                 # e + U+0301 combining acute (2 code points)
let nfc = unicode.normalize(nfd, "NFC");
io.println(nfc.length());          # 1 - now a single code point
io.println(unicode.normalize("ﬁ", "NFKC"));   # fi - ligature decomposed
```

| Function | Returns | Description |
|----------|---------|-------------|
| `unicode.normalize(s, form)` | `string` | A copy of `s` normalised under `form`. Throws on an unknown form. |
| `unicode.isNormalized(s, form)` | `bool` | True when `s` is already in `form`. Cheap; does not allocate a normalised copy. |

### When to use which form

| Form | Effect | Typical use |
|------|--------|-------------|
| **NFC** | Canonical composition. Combining marks fold into precomposed code points where one exists. | Storage, display, equality comparison of "the same character" inputs. The Web's standard. |
| **NFD** | Canonical decomposition. Precomposed characters split into base + combining marks. | Sorting that respects diacritics, accent-insensitive search after stripping marks. |
| **NFKC** | Compatibility composition. Compatibility equivalents (ligatures, full-width, superscripts) fold to their base form, then canonical composition is applied. | Search across visually-similar characters; input sanitisation. |
| **NFKD** | Compatibility decomposition. Same compatibility folding as NFKC but no recomposition. | The fully decomposed canonical form; rarely needed directly. |

Normalising untrusted input before storing or comparing is good
defensive practice: it stops bypass attacks that rely on visually
identical but byte-different strings (`"admin"` vs `"admın"`
with a Turkish dotless i, for example - NFKC won't collapse
that, but normalising at least makes equality reliable).

---

## Templates: `template`

The `template` module is backed by Go's `html/template`: a full templating
engine with data binding, conditionals, loops, and pipelines, plus
**contextual auto-escaping** - interpolated values are HTML-escaped for the
position they appear in (element text, attribute, URL, script), so the engine
is XSS-safe by default. (For escaping a single string outside a template, see
`encoding.htmlEscape`; for sanitizing untrusted HTML, `encoding.sanitizeHtml`.)

Module functions:

- `renderString(source, data)` - render a template string against `data`,
  returning the result string.
- `Template(source[, name])` - compile a reusable `Template` value.
- `load(path)` - read and compile a template from a file.
- `Engine(dir)` - a `TemplateEngine` rooted at a directory; accepts a string
  path or an options dict (`{"dir": ...}`).

`Template` methods: `render(data)`, `name()`, `path()`, `toString()`.
`TemplateEngine` methods: `render(name, data)` (loads `<dir>/<name>` and
renders), `load(name)` (returns a `Template`), `dir()`.

### Syntax

Data is supplied as a dict (or any value); fields are referenced with a leading
dot:

```gb
import template;
import io;

io.println(template.renderString("Hello {{.name}}", {"name": "Ada"}));
io.println(template.renderString("{{.user.email}}",
    {"user": {"email": "ada@example.com"}}));
```

Common actions:

- Conditionals: `{{if .admin}}Admin{{else}}Guest{{end}}`
- Iteration: `{{range .items}}<li>{{.}}</li>{{end}}` (inside `range`, `.` is the
  current element; `{{range $i, $v := .items}}` binds index and value).
- Scoping: `{{with .profile}}{{.bio}}{{end}}`
- Pipelines: `{{.price | printf "%.2f"}}`
- Comments: `{{/* not rendered */}}`

```gb
let tmpl = template.Template(
    "<ul>{{range .todos}}<li>{{.title}}</li>{{end}}</ul>");
io.println(tmpl.render({"todos": [{"title": "ship"}, {"title": "rest"}]}));
```

Auto-escaping means untrusted data is safe to interpolate directly; the engine
escapes `<`, `>`, `&`, quotes, and URL/script context as needed. To emit
pre-trusted HTML verbatim, mark it with the engine's standard mechanisms rather
than disabling escaping.

A directory-backed engine keeps templates on disk:

```gb
let engine = template.Engine("templates");
io.println(engine.render("welcome.html", {"name": "Ada"}));
```
