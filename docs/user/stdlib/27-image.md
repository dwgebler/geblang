# Image (`image`)

A portable native raster-image module. No system library is required;
it is backed entirely by Go's standard image packages. PNG, JPEG,
GIF, and WebP decoding are supported in every normal Geblang build.

Load and transform existing images:

```gb
import image;

let img = image.load("photo.jpg");
let thumb = img.resize(128, 128);
thumb.save("thumb.png", "png");
img.close();
thumb.close();
```

Or create one from scratch and draw on it:

```gb
import image;

let img = image.blank(240, 160, "white");
img.rect(10, 10, 80, 40, {"fill": "#3366ff"})
   .circle(150, 60, 30, {"color": "red", "width": 3})
   .line(0, 0, 239, 159, {"color": "black"})
   .polygon([[10, 100], [60, 70], [90, 110]], {"fill": "#00aa0080", "color": "green"})
   .text("Hello", 100, 120, {"size": 18, "font": "bold", "color": "#222"});
img.save("out.png", "png");
```

For QR codes and barcodes, see the [barcode](barcode.html) module, which
returns ordinary `Image` values.

## Factories

### `image.load(string path): Image`

Loads and decodes a file from disk. The format is detected
automatically. Throws `RuntimeError` if the file cannot be read
or the format is not recognised.

### `image.loadBytes(bytes data): Image`

Decodes an image from raw bytes already in memory. Useful when
the image has been fetched over HTTP or read from an archive.
Throws `RuntimeError` on an unrecognised format.

### `image.blank(int w, int h, ?string fill = null): Image`

Creates a new image of the given dimensions. It is fully transparent
unless `fill` names a color (see [Colors](#colors)).

```gb
let clear = image.blank(64, 64);
let page = image.blank(800, 600, "white");
```

### `image.loadFont(string path): Font`

Loads a TrueType or OpenType font file (`.ttf`, `.otf`) and returns a
`Font` to pass as the `font` option of `text` and `measureText`. Throws
`RuntimeError` if the data is not a supported font. Font collections
(`.ttc`) are not supported.

### `image.loadFontBytes(bytes data): Font`

The same as `loadFont` for font data already in memory.

### `image.measureText(string content, dict<string, any> opts = {}): dict<string, any>`

Returns `{"width": int, "height": int}`: the pixel size `content` would
occupy when drawn with the same `font` and `size` options as the
`text` method.

```gb
let size = image.measureText("Total: 42", {"size": 20, "font": "bold"});
let x = (img.width() - (size["width"] as int)) / 2;
```

### `image.rgba(int r, int g, int b, int a = 255): string`

Builds a `"#rrggbbaa"` color string from channel values between 0 and
255. Throws `RuntimeError` for a channel outside that range.

## Colors

Every color argument and option is a string in one of these forms:

| Form | Example | Meaning |
|------|---------|---------|
| `#rgb` | `"#f00"` | short hex, opaque |
| `#rgba` | `"#f008"` | short hex with alpha |
| `#rrggbb` | `"#ff0000"` | hex, opaque |
| `#rrggbbaa` | `"#ff000080"` | hex with alpha |
| name | `"red"` | a named color |

Named colors: `black`, `white`, `red`, `green`, `lime`, `blue`, `yellow`,
`cyan`, `magenta`, `orange`, `purple`, `pink`, `brown`, `gray` (or `grey`),
`silver`, `navy`, `teal`, `maroon`, `olive`, and `transparent`. Names and
hex digits are case-insensitive. An unrecognised color throws
`RuntimeError`.

Colors with alpha below `ff` blend over what is already on the image.
`getPixel` always returns the `#rrggbbaa` form.

## `Image` class

Methods fall into two groups:

- **Transforms** (`resize`, `crop`, `rotate`, `flipH`, `flipV`, `copy`)
  return a **new** `Image`; the source is unchanged and remains valid.
- **Drawing methods** (`setPixel`, `fill`, `line`, `rect`, `circle`,
  `ellipse`, `arc`, `polygon`, `paste`, `text`) modify the image **in
  place** and return the same `Image`, so calls chain. Use `copy()` first
  to keep the original.

Call `close()` on each handle you no longer need, or let the process
exit reclaim everything.

### `width(): int`

Returns the image width in pixels.

### `height(): int`

Returns the image height in pixels.

### `resize(int w, int h): Image`

Scales the image to `w` x `h` pixels using CatmullRom (bicubic)
resampling. Returns a new `Image`.

### `crop(int x, int y, int w, int h): Image`

Extracts a rectangle at offset `(x, y)` with dimensions `w` x
`h`. Returns a new `Image`. Throws `RuntimeError` if the
rectangle extends outside the source bounds.

### `rotate(int degrees): Image`

Rotates the image. `degrees` must be a multiple of 90; any other
value throws `RuntimeError`. Returns a new `Image`.

### `flipH(): Image`

Returns a new `Image` mirrored left-to-right.

### `flipV(): Image`

Returns a new `Image` mirrored top-to-bottom.

### `copy(): Image`

Returns an independent copy. Drawing on the copy does not affect the
source.

### `encode(string format): bytes`

Encodes the image to the given format and returns the result as
raw bytes. `format` must be one of `"png"`, `"jpeg"` (or `"jpg"`),
or `"gif"`. WebP is supported for decoding only - pass `"png"` to
re-encode a WebP source.

### `save(string path, string format): void`

Encodes the image and writes it to `path`. Equivalent to encoding
and then writing the bytes to disk. The same format strings apply
as for `encode`.

### `close(): void`

Releases the native handle. After `close()` the `Image` value
must not be used. Calling `close()` is optional but recommended
for long-running programs that process many images.

## Drawing

Coordinates are in pixels with the origin at the top-left; `x` grows
right and `y` grows down. Coordinates and sizes accept `int`, `float`,
or `decimal` values. Shapes are anti-aliased, and anything outside the
image is clipped.

Shape methods take a trailing options dict:

| Option | Meaning | Default |
|--------|---------|---------|
| `color` | outline color | `"black"` when `fill` is absent |
| `fill` | interior color | none |
| `width` | outline thickness in pixels | `1` |

With only `fill`, the shape is filled and has no outline. With both
`fill` and `color`, it is filled and then outlined. Invalid input
(an unknown color, a non-positive size, a malformed point list) throws
`RuntimeError`.

### `getPixel(int x, int y): string`

Returns the pixel as `"#rrggbbaa"`. Throws `RuntimeError` when the
position is outside the image.

### `setPixel(int x, int y, string color): Image`

Replaces one pixel exactly; no blending is applied.

### `fill(string color): Image`

Replaces every pixel with `color`.

### `line(any x1, any y1, any x2, any y2, dict<string, any> opts = {}): Image`

Draws a line between two pixels, inclusive of both. Options: `color`,
`width`.

### `rect(any x, any y, any w, any h, dict<string, any> opts = {}): Image`

Draws a rectangle with its top-left at `(x, y)`. The outline is drawn
inside the rectangle, so a `w` x `h` rectangle never touches pixels
outside that area.

### `circle(any cx, any cy, any radius, dict<string, any> opts = {}): Image`

Draws a circle centred on `(cx, cy)`.

### `ellipse(any cx, any cy, any rx, any ry, dict<string, any> opts = {}): Image`

Draws an ellipse centred on `(cx, cy)` with horizontal radius `rx` and
vertical radius `ry`.

### `arc(any cx, any cy, any radius, any startDegrees, any endDegrees, dict<string, any> opts = {}): Image`

Draws part of a circle's outline. Angles are in degrees, measured
clockwise from 3 o'clock. With `fill`, the arc is closed to the centre
and drawn as a pie slice.

```gb
img.arc(100, 100, 40, 0, 90, {"fill": "orange"});          # pie slice
img.arc(100, 100, 40, 90, 360, {"color": "gray", "width": 6});
```

### `polygon(list<any> points, dict<string, any> opts = {}): Image`

Draws a polygon through a list of `[x, y]` points (at least three).
Set the extra option `"closed": false` to draw an open polyline through
two or more points instead.

```gb
img.polygon([[10, 90], [50, 10], [90, 90]], {"fill": "teal"});
img.polygon([[0, 50], [25, 20], [50, 60], [75, 30]], {"color": "red", "width": 2, "closed": false});
```

### `paste(Image source, int x, int y, dict<string, any> opts = {}): Image`

Composites `source` onto this image with its top-left at `(x, y)`,
respecting the source's transparency. The option `opacity` (0 to 1,
default 1) fades the source. Parts falling outside this image are
clipped.

```gb
let watermark = image.load("logo.png").resize(64, 64);
photo.paste(watermark, photo.width() - 72, photo.height() - 72, {"opacity": 0.6});
```

### `text(string content, any x, any y, dict<string, any> opts = {}): Image`

Draws text with the top-left corner of its box at `(x, y)`. A newline in
`content` starts a new line.

| Option | Meaning | Default |
|--------|---------|---------|
| `font` | `"regular"`, `"bold"`, `"italic"`, `"mono"`, or a `Font` from `image.loadFont` | `"regular"` |
| `size` | font size in points (one point is one pixel) | `16` |
| `color` | text color | `"black"` |

The four named fonts are the Go font family and are built in, so text
works without any font file.

```gb
let brand = image.loadFont("fonts/Brand.ttf");
img.text("Invoice 1042", 20, 20, {"font": brand, "size": 28})
   .text("due in 30 days", 20, 56, {"font": "italic", "color": "#666"});
```

## Supported formats

| Format | Decode | Encode |
|--------|--------|--------|
| PNG    | yes    | yes    |
| JPEG   | yes    | yes    |
| GIF    | yes    | yes    |
| WebP   | yes    | no     |

WebP images can be loaded with `image.load` or `image.loadBytes`,
but `encode` and `save` do not support `"webp"` as a format
argument.

## Example: load, transform, and save

```gb
import image;
import io;

let src = image.load("input.png");
io.println("source: ${src.width()}x${src.height()}");

/* crop a central region, then scale it down */
let cx = src.width() / 4;
let cy = src.height() / 4;
let cropped = src.crop(cx, cy, src.width() / 2, src.height() / 2);
let out = cropped.resize(256, 256);

out.save("output.png", "png");
io.println("saved output.png");

src.close();
cropped.close();
out.close();
```

## Example: encode to bytes

```gb
import image;
import io;

let img = image.blank(64, 64);
let data = img.encode("png");
io.println("png bytes: ${data.length()}");
img.close();
```

## Handle lifetime

- Each factory (`load`, `loadBytes`, `blank`) and each transform
  (`resize`, `crop`, `rotate`, `flipH`, `flipV`, `copy`) returns an
  independent `Image` handle. Drawing methods return the image they were
  called on, not a new handle.
- Closing a source does not affect any derived images.
- In short scripts and CLI tools, skipping `close()` is fine; the
  OS reclaims all handles on exit.
- In long-running programs that process many images, call `close()`
  on each handle once it is no longer needed.
