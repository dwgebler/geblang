# Barcodes and QR Codes (`barcode`)

Generates QR codes and other barcodes as [`image.Image`](image.html)
values. No system library is required. Because the result is an ordinary
image, it can be saved, resized, drawn on, or pasted onto another image.

```gb
import barcode;

let qr = barcode.qr("https://example.com", {"scale": 8, "level": "M"});
qr.save("qr.png", "png");

let ean = barcode.encode("5901234123457", {"format": "ean13", "width": 300, "height": 120});
ean.save("ean.png", "png");
```

## Functions

### `barcode.encode(string data, dict<string, any> opts = {}): image.Image`

Encodes `data` in the symbology named by the required `format` option.
Throws `RuntimeError` when the format is unknown, when `data` is empty or
not valid for the format (wrong length, unsupported characters, a wrong
check digit, too much data), or when an option is invalid.

### `barcode.qr(string data, dict<string, any> opts = {}): image.Image`

Shortcut for `barcode.encode(data, {...opts, "format": "qr"})`.

### `barcode.formats(): list<string>`

Returns the supported format names, sorted.

## Formats

| Format | Kind | Accepts |
|--------|------|---------|
| `qr` | 2D | any text; numeric, alphanumeric and UTF-8 modes are chosen automatically |
| `datamatrix` | 2D | any text |
| `aztec` | 2D | any text |
| `pdf417` | 2D (stacked) | any text |
| `code128` | linear | ASCII text |
| `code39` | linear | upper-case letters, digits, space and `-.$/+%` |
| `code93` | linear | upper-case letters, digits, space and `-.$/+%` |
| `codabar` | linear | digits and `-$:/.+` between start/stop letters `A`-`D` |
| `ean13` | linear | 12 digits, or 13 including the check digit |
| `ean8` | linear | 7 digits, or 8 including the check digit |
| `upca` | linear | 11 digits, or 12 including the check digit |
| `itf` | linear | an even number of digits (Interleaved 2 of 5) |

For `ean13`, `ean8` and `upca` the check digit is computed when omitted
and verified when supplied.

## Options

Sizing and styling apply to every format:

| Option | Meaning | Default |
|--------|---------|---------|
| `format` | symbology name (required for `encode`) | none |
| `scale` | pixels per module (the narrowest bar or one 2D cell) | `4` for 2D, `2` for linear |
| `margin` | quiet zone around the code, in modules | `4` for 2D, `10` for linear |
| `width` | exact image width in pixels | sized from `scale` |
| `height` | exact image height in pixels | sized from `scale`; bars are 80 pixels tall for linear codes |
| `color` | color of the bars or cells | `"black"` |
| `background` | background color | `"white"` |

When `width` or `height` is given, the code is drawn at the largest whole
`scale` that fits and centred on a canvas of exactly that size, so
modules stay crisp. A size too small to hold the code throws
`RuntimeError`, as does a result larger than 16384 pixels on a side. Colors use the same forms as the
[image module](image.html#colors); `"transparent"` is allowed as a
background.

Format-specific options:

| Option | Formats | Meaning | Default |
|--------|---------|---------|---------|
| `level` | `qr` | error correction: `"L"` (7%), `"M"` (15%), `"Q"` (25%), `"H"` (30%) | `"M"` |
| `level` | `pdf417` | security level, `0` to `8` | `2` |
| `level` | `aztec` | error-correction percentage, `5` to `95` | `33` |
| `checksum` | `code39`, `code93` | append the check character(s) | `false` for `code39`, `true` for `code93` |
| `fullAscii` | `code39`, `code93` | allow the full ASCII set | `false` |

Scanners need dark bars on a light background and an intact quiet zone;
keep `margin` at its default unless the surrounding artwork already
provides the space.

## Example: a labelled QR code

```gb
import barcode;
import image;

let qr = barcode.qr("https://example.com/order/1042", {"scale": 6, "level": "Q"});
let caption = "Order 1042";
let size = image.measureText(caption, {"size": 18, "font": "bold"});

let label = image.blank(qr.width(), qr.height() + 40, "white");
label.paste(qr, 0, 0)
     .text(caption, (qr.width() - (size["width"] as int)) / 2, qr.height() + 8, {"size": 18, "font": "bold"});
label.save("label.png", "png");
```

## Example: serving a barcode as bytes

```gb
import barcode;

func ticketPng(string code): bytes {
    return barcode.encode(code, {"format": "code128", "scale": 3}).encode("png");
}
```
