# Localization And Translation

Import `locale` for formatting and collation, and `i18n` for message catalogs.
Every call takes an explicit BCP 47 locale tag. The initial supported language
families are English (`en`), French (`fr`), and German (`de`), including regional
tags such as `en-GB`, `fr-CA`, and `de-CH`. The APIs do not change process-wide
locale or time zone settings.

The implementation pins `golang.org/x/text v0.37.0` and CLDR 32. Date patterns,
names, number symbols, and currency placement are a selected copy of
[Unicode CLDR 32](https://github.com/unicode-cldr/cldr-dates-full/tree/32.0.0)
and its [number data](https://github.com/unicode-cldr/cldr-numbers-full/tree/32.0.0).
The selected data is modified for this runtime; its Unicode license and
provenance notice ship beside the data file.
Collation, currency symbols and fraction digits, and plural rules come from the
pinned Go package. Tags outside the three supported language families raise an
error.

## Numbers And Currency

| Function | Description |
| --- | --- |
| `locale.formatNumber(value, tag, opts = {})` | Format an int, decimal, or finite float. |
| `locale.formatCurrency(value, currency, tag, opts = {})` | Format with an ISO 4217 currency code and its usual fraction digits. |

`opts` accepts `minFractionDigits`, `maxFractionDigits` (integers from 0 to
100), and `grouping` (boolean, default true). Number formatting defaults to
zero through three fractional digits; trailing zeroes are removed down to the
minimum. Currency formatting defaults to the currency's standard fraction
count, such as two for EUR and zero for JPY. Overrides apply to either API.
Decimal values retain their exact digits during rounding and formatting.

```gb
import locale;

io.println(locale.formatNumber(12345.67, "de"));          /* 12.345,67 */
io.println(locale.formatCurrency(1234.5, "EUR", "fr")); /* amount then euro sign */
```

Invalid tags, unknown currency codes, nonnumeric values, and conflicting digit
limits raise catchable errors.

## Dates And Collation

`locale.formatDate(instant, tag, style = "medium", zone = "UTC")` converts a
`datetime.Instant` into `zone`, then formats its calendar date using the CLDR
`short`, `medium`, `long`, or `full` style. An invalid time zone or style is an
error. Explicit values and the pinned data produce deterministic output.

`locale.compare(a, b, tag, opts = {})` returns a negative, zero, or positive
integer in locale collation order. Set `ignoreCase` or `numeric` to true in
`opts` when needed. `numeric` orders `"file2"` before `"file10"`.

```gb
import datetime;
import locale;

let instant = datetime.Instant("2024-07-04T23:30:00Z");
io.println(locale.formatDate(instant, "en-GB", "full", "Europe/London"));
/* Friday, 5 July 2024 */
io.println(locale.compare("file2", "file10", "en", {"numeric": true}) < 0);
```

`locale.canonicalTag(tag)` returns the canonical BCP 47 spelling.
`locale.pluralCategory(count, tag)` returns a CLDR cardinal category for an
integer count; it is also used by `i18n.Catalog`.

## Message Catalogs

`i18n.catalog(messages, tag, fallback = "en")` returns an immutable
`Catalog`. `messages` maps locale tags to message dictionaries. A message is
plain text or a dictionary of CLDR plural categories such as `one` and
`other`. Lookup checks the requested regional tag, its language, then the
explicit fallback tag and its language. A missing key after fallback is an
error. The catalog copies and freezes the supplied dictionaries, so callers
can reuse and change their input without changing existing catalogs.

`Catalog.text(key, args = {})` interpolates named placeholders such as
`{name}`. `Catalog.plural(key, count, args = {})` selects the locale's plural
category, or `other` when the chosen category is absent, and interpolates
`{count}`. A missing placeholder argument or missing required plural form
raises an error; extra arguments are ignored. Double braces `{{` and `}}`
produce literal braces. Message text is plain text with no HTML escaping or
template execution.

```gb
import i18n;

let messages = {
    "en": {"files": {"one": "{count} file", "other": "{count} files"}},
    "fr": {"files": {"one": "{count} fichier", "other": "{count} fichiers"}}
};
let french = i18n.catalog(messages, "fr-CA");
io.println(french.plural("files", 2)); /* 2 fichiers */
```

Create a catalog for each request or CLI locale selection. Catalogs with
different tags can be used concurrently without shared mutable settings.
