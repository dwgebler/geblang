package native

import (
	"strings"
	"testing"

	"geblang/internal/runtime"
)

func imgInt(n int64) runtime.Value  { return runtime.SmallInt{Value: n} }
func imgStr(s string) runtime.Value { return runtime.String{Value: s} }

func imgOpts(pairs ...any) runtime.Value {
	d := runtime.NewDict()
	for i := 0; i < len(pairs); i += 2 {
		k := runtime.String{Value: pairs[i].(string)}
		d.PutEntry(DictKey(k), runtime.DictEntry{Key: k, Value: pairs[i+1].(runtime.Value)})
	}
	return d
}

func imgPixel(t *testing.T, r *Registry, h runtime.Value, x, y int64) string {
	t.Helper()
	return callImage(t, r, "getPixel", h, imgInt(x), imgInt(y)).(runtime.String).Value
}

func imgErr(t *testing.T, r *Registry, name, want string, args ...runtime.Value) {
	t.Helper()
	_, err := r.Call("imagenative", name, args)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("image.%s error = %v, want containing %q", name, err, want)
	}
}

func TestParseImageColor(t *testing.T) {
	cases := map[string]string{
		"#f00":        "#ff0000ff",
		"#f008":       "#ff000088",
		"#3366FF":     "#3366ffff",
		"#00aa0080":   "#00aa0080",
		" Red ":       "#ff0000ff",
		"transparent": "#00000000",
	}
	for in, want := range cases {
		c, err := parseImageColor(in)
		if err != nil || formatImageColor(c) != want {
			t.Errorf("parseImageColor(%q) = %s, %v; want %s", in, formatImageColor(c), err, want)
		}
	}
	for _, bad := range []string{"", "#", "#12", "#12345", "#gggggg", "notacolor", "ff0000"} {
		if _, err := parseImageColor(bad); err == nil {
			t.Errorf("parseImageColor(%q) accepted an invalid color", bad)
		}
	}
}

func TestImageBlankFillAndPixels(t *testing.T) {
	r := NewBuiltinRegistry()
	h := callImage(t, r, "blank", imgInt(8), imgInt(6), imgStr("white"))
	if got := imgPixel(t, r, h, 7, 5); got != "#ffffffff" {
		t.Fatalf("filled blank pixel = %s", got)
	}
	clear := callImage(t, r, "blank", imgInt(2), imgInt(2))
	if got := imgPixel(t, r, clear, 0, 0); got != "#00000000" {
		t.Fatalf("default blank pixel = %s", got)
	}
	callImage(t, r, "setPixel", h, imgInt(3), imgInt(2), imgStr("#11223344"))
	if got := imgPixel(t, r, h, 3, 2); got != "#11223344" {
		t.Fatalf("setPixel round trip = %s", got)
	}
	callImage(t, r, "fill", h, imgStr("navy"))
	if got := imgPixel(t, r, h, 3, 2); got != "#000080ff" {
		t.Fatalf("fill = %s", got)
	}
	imgErr(t, r, "getPixel", "outside the image", h, imgInt(8), imgInt(0))
	imgErr(t, r, "setPixel", "outside the image", h, imgInt(-1), imgInt(0), imgStr("red"))
	imgErr(t, r, "fill", "invalid color", h, imgStr("nope"))
	imgErr(t, r, "blank", "invalid color", imgInt(2), imgInt(2), imgStr("#zz"))
}

func TestImageShapesAreCrispOnPixelGrid(t *testing.T) {
	r := NewBuiltinRegistry()
	h := callImage(t, r, "blank", imgInt(40), imgInt(40), imgStr("white"))

	callImage(t, r, "rect", h, imgInt(4), imgInt(4), imgInt(10), imgInt(8), imgOpts("fill", imgStr("red")))
	for _, p := range [][2]int64{{4, 4}, {13, 11}} {
		if got := imgPixel(t, r, h, p[0], p[1]); got != "#ff0000ff" {
			t.Errorf("rect fill at %v = %s", p, got)
		}
	}
	for _, p := range [][2]int64{{3, 4}, {14, 4}, {4, 12}} {
		if got := imgPixel(t, r, h, p[0], p[1]); got != "#ffffffff" {
			t.Errorf("rect fill leaked to %v = %s", p, got)
		}
	}

	callImage(t, r, "rect", h, imgInt(20), imgInt(4), imgInt(10), imgInt(10), imgOpts("color", imgStr("blue"), "width", imgInt(2)))
	if got := imgPixel(t, r, h, 21, 5); got != "#0000ffff" {
		t.Errorf("rect stroke = %s", got)
	}
	if got := imgPixel(t, r, h, 25, 9); got != "#ffffffff" {
		t.Errorf("stroked rect interior was filled: %s", got)
	}

	callImage(t, r, "line", h, imgInt(2), imgInt(30), imgInt(12), imgInt(30))
	for _, x := range []int64{2, 7, 12} {
		if got := imgPixel(t, r, h, x, 30); got != "#000000ff" {
			t.Errorf("default line at x=%d = %s", x, got)
		}
	}
	if got := imgPixel(t, r, h, 7, 29); got != "#ffffffff" {
		t.Errorf("1px line bled to the row above: %s", got)
	}

	callImage(t, r, "ellipse", h, imgInt(30), imgInt(30), imgInt(6), imgInt(6), imgOpts("fill", imgStr("lime")))
	if got := imgPixel(t, r, h, 30, 30); got != "#00ff00ff" {
		t.Errorf("circle centre = %s", got)
	}
	if got := imgPixel(t, r, h, 37, 37); got != "#ffffffff" {
		t.Errorf("circle bounding corner was painted: %s", got)
	}
}

func TestImagePolygonArcAndAlphaBlend(t *testing.T) {
	r := NewBuiltinRegistry()
	h := callImage(t, r, "blank", imgInt(40), imgInt(40), imgStr("white"))
	pts := &runtime.List{Elements: []runtime.Value{
		&runtime.List{Elements: []runtime.Value{imgInt(5), imgInt(5)}},
		&runtime.List{Elements: []runtime.Value{imgInt(35), imgInt(5)}},
		&runtime.List{Elements: []runtime.Value{runtime.Float{Value: 20.5}, imgInt(35)}},
	}}
	callImage(t, r, "polygon", h, pts, imgOpts("fill", imgStr("#00000080")))
	if got := imgPixel(t, r, h, 20, 12); got != "#7f7f7fff" {
		t.Errorf("half-alpha black over white = %s", got)
	}
	if got := imgPixel(t, r, h, 2, 30); got != "#ffffffff" {
		t.Errorf("polygon painted outside: %s", got)
	}

	arc := callImage(t, r, "blank", imgInt(40), imgInt(40), imgStr("white"))
	callImage(t, r, "arc", arc, imgInt(20), imgInt(20), imgInt(12), imgInt(0), imgInt(90), imgOpts("fill", imgStr("red")))
	if got := imgPixel(t, r, arc, 25, 25); got != "#ff0000ff" {
		t.Errorf("pie slice lower-right quadrant = %s", got)
	}
	if got := imgPixel(t, r, arc, 15, 15); got != "#ffffffff" {
		t.Errorf("pie slice painted the opposite quadrant: %s", got)
	}

	imgErr(t, r, "polygon", "at least 3 points", h, &runtime.List{Elements: pts.Elements[:2]})
	imgErr(t, r, "polygon", "list of [x, y] pairs", h, &runtime.List{Elements: []runtime.Value{imgInt(1)}})
	imgErr(t, r, "rect", "must be positive", h, imgInt(0), imgInt(0), imgInt(0), imgInt(4))
	imgErr(t, r, "rect", "\"width\" must be positive", h, imgInt(0), imgInt(0), imgInt(4), imgInt(4), imgOpts("width", imgInt(0)))
	imgErr(t, r, "line", "options must be a dict", h, imgInt(0), imgInt(0), imgInt(4), imgInt(4), imgStr("red"))
}

func TestImageDrawingClipsOutOfBoundsGeometry(t *testing.T) {
	r := NewBuiltinRegistry()
	h := callImage(t, r, "blank", imgInt(10), imgInt(10), imgStr("white"))
	callImage(t, r, "line", h, imgInt(-50), imgInt(5), imgInt(60), imgInt(5), imgOpts("color", imgStr("red")))
	callImage(t, r, "ellipse", h, imgInt(100), imgInt(100), imgInt(5), imgInt(5), imgOpts("fill", imgStr("red")))
	callImage(t, r, "rect", h, imgInt(-5), imgInt(-5), imgInt(7), imgInt(7), imgOpts("fill", imgStr("blue")))
	if got := imgPixel(t, r, h, 9, 5); got != "#ff0000ff" {
		t.Errorf("clipped line = %s", got)
	}
	if got := imgPixel(t, r, h, 1, 1); got != "#0000ffff" {
		t.Errorf("clipped rect = %s", got)
	}
	if got := imgPixel(t, r, h, 9, 9); got != "#ffffffff" {
		t.Errorf("off-canvas ellipse painted: %s", got)
	}
}

func TestImageCopyFlipPaste(t *testing.T) {
	r := NewBuiltinRegistry()
	h := callImage(t, r, "blank", imgInt(4), imgInt(2), imgStr("white"))
	callImage(t, r, "setPixel", h, imgInt(0), imgInt(0), imgStr("red"))

	cp := callImage(t, r, "copy", h)
	callImage(t, r, "fill", cp, imgStr("black"))
	if got := imgPixel(t, r, h, 0, 0); got != "#ff0000ff" {
		t.Fatalf("mutating a copy changed the source: %s", got)
	}

	fh := callImage(t, r, "flip", h, imgStr("h"))
	fv := callImage(t, r, "flip", h, imgStr("v"))
	if imgPixel(t, r, fh, 3, 0) != "#ff0000ff" || imgPixel(t, r, fv, 0, 1) != "#ff0000ff" {
		t.Fatalf("flip moved the marker to the wrong pixel")
	}
	imgErr(t, r, "flip", "direction", h, imgStr("x"))

	dst := callImage(t, r, "blank", imgInt(6), imgInt(6), imgStr("white"))
	stamp := callImage(t, r, "blank", imgInt(2), imgInt(2), imgStr("black"))
	callImage(t, r, "paste", dst, stamp, imgInt(1), imgInt(1))
	callImage(t, r, "paste", dst, stamp, imgInt(4), imgInt(4), imgOpts("opacity", runtime.Float{Value: 0.5}))
	callImage(t, r, "paste", dst, stamp, imgInt(5), imgInt(-1))
	if got := imgPixel(t, r, dst, 2, 2); got != "#000000ff" {
		t.Errorf("opaque paste = %s", got)
	}
	if got := imgPixel(t, r, dst, 4, 4); got != "#7f7f7fff" && got != "#808080ff" {
		t.Errorf("half-opacity paste = %s", got)
	}
	if got := imgPixel(t, r, dst, 5, 0); got != "#000000ff" {
		t.Errorf("partially off-canvas paste = %s", got)
	}
	if got := imgPixel(t, r, dst, 3, 3); got != "#ffffffff" {
		t.Errorf("paste bled outside its rectangle: %s", got)
	}
	callImage(t, r, "paste", dst, dst, imgInt(3), imgInt(0))
	if got := imgPixel(t, r, dst, 5, 2); got != "#000000ff" {
		t.Errorf("self-paste = %s", got)
	}
	imgErr(t, r, "paste", "between 0 and 1", dst, stamp, imgInt(0), imgInt(0), imgOpts("opacity", imgInt(2)))
}

func TestImageDrawOnDecodedImageConvertsInPlace(t *testing.T) {
	r := NewBuiltinRegistry()
	src := callImage(t, r, "blank", imgInt(6), imgInt(6), imgStr("white"))
	gif := callImage(t, r, "encode", src, imgStr("gif"))
	h := callImage(t, r, "decode", gif)
	callImage(t, r, "rect", h, imgInt(1), imgInt(1), imgInt(3), imgInt(3), imgOpts("fill", imgStr("#12ab34")))
	if got := imgPixel(t, r, h, 2, 2); got != "#12ab34ff" {
		t.Fatalf("drawing on a decoded paletted image = %s", got)
	}
}

func TestImageTextAndMeasure(t *testing.T) {
	r := NewBuiltinRegistry()
	size := func(text string, opts runtime.Value) (int64, int64) {
		d := callImage(t, r, "measureText", imgStr(text), opts).(runtime.Dict)
		w, _ := dictLookup(d, "width")
		h, _ := dictLookup(d, "height")
		return w.(runtime.SmallInt).Value, h.(runtime.SmallInt).Value
	}
	w1, h1 := size("Hello", imgOpts("size", imgInt(20)))
	w2, h2 := size("Hello Hello", imgOpts("size", imgInt(20)))
	_, h3 := size("Hello\nHello", imgOpts("size", imgInt(20)))
	wBig, _ := size("Hello", imgOpts("size", imgInt(40)))
	if w1 <= 0 || h1 <= 0 || w2 <= w1 || h2 != h1 || h3 <= h1 || wBig <= w1 {
		t.Fatalf("measureText inconsistent: %d %d %d %d %d %d", w1, h1, w2, h2, h3, wBig)
	}
	wMonoI, _ := size("iiii", imgOpts("font", imgStr("mono")))
	wMonoM, _ := size("MMMM", imgOpts("font", imgStr("mono")))
	if wMonoI != wMonoM {
		t.Fatalf("mono font is not fixed pitch: %d vs %d", wMonoI, wMonoM)
	}

	h := callImage(t, r, "blank", imgInt(w1+4), imgInt(h1+4), imgStr("white"))
	callImage(t, r, "text", h, imgStr("Hello"), imgInt(2), imgInt(2), imgOpts("size", imgInt(20)))
	dark := 0
	for y := int64(0); y < h1+4; y++ {
		for x := int64(0); x < w1+4; x++ {
			if imgPixel(t, r, h, x, y) == "#000000ff" {
				dark++
			}
		}
	}
	if dark < 20 {
		t.Fatalf("text drew only %d solid pixels", dark)
	}
	for x := int64(0); x < w1+4; x++ {
		if imgPixel(t, r, h, x, 0) != "#ffffffff" {
			t.Fatalf("text painted above its top-left anchor at x=%d", x)
		}
	}

	font := callImage(t, r, "loadFont", runtime.Bytes{Value: imageBuiltinFontData["bold"]})
	wLoaded, _ := size("Hello", imgOpts("font", font, "size", imgInt(20)))
	wBold, _ := size("Hello", imgOpts("font", imgStr("bold"), "size", imgInt(20)))
	if wLoaded != wBold {
		t.Fatalf("loaded font measures %d, built-in bold %d", wLoaded, wBold)
	}
	imgErr(t, r, "loadFont", "not a TrueType or OpenType font", runtime.Bytes{Value: []byte("nope")})
	imgErr(t, r, "measureText", "unknown font", imgStr("x"), imgOpts("font", imgStr("comic")))
	imgErr(t, r, "measureText", "\"size\"", imgStr("x"), imgOpts("size", imgInt(0)))
	imgErr(t, r, "text", "not a Font", h, imgStr("x"), imgInt(0), imgInt(0), imgOpts("font", h))
}

func TestBarcodeFormatsAndSizing(t *testing.T) {
	r := NewBuiltinRegistry()
	dims := func(h runtime.Value) (int64, int64) {
		return callImage(t, r, "width", h).(runtime.SmallInt).Value, callImage(t, r, "height", h).(runtime.SmallInt).Value
	}
	samples := map[string]string{
		"aztec": "Hello", "codabar": "A1234B", "code128": "Hello-128", "code39": "HELLO 39",
		"code93": "HELLO93", "datamatrix": "Hello", "ean13": "590123412345", "ean8": "9638507",
		"itf": "12345678", "pdf417": "Hello", "qr": "Hello", "upca": "03600029145",
	}
	names := callImage(t, r, "barcodeFormats").(*runtime.List)
	if len(names.Elements) != len(samples) {
		t.Fatalf("formats() lists %d names, test covers %d", len(names.Elements), len(samples))
	}
	for _, n := range names.Elements {
		name := n.(runtime.String).Value
		h := callImage(t, r, "barcode", imgStr(samples[name]), imgOpts("format", imgStr(name)))
		w, ht := dims(h)
		if w < 20 || ht < 20 {
			t.Errorf("%s rendered %dx%d", name, w, ht)
		}
		if got := imgPixel(t, r, h, 0, 0); got != "#ffffffff" {
			t.Errorf("%s quiet zone pixel = %s", name, got)
		}
	}

	// A version-1 QR code is 21 modules; the default quiet zone adds 4 each side.
	qr := callImage(t, r, "barcode", imgStr("HI"), imgOpts("format", imgStr("qr"), "scale", imgInt(3)))
	if w, h := dims(qr); w != 29*3 || h != 29*3 {
		t.Fatalf("qr scale 3 = %dx%d, want 87x87", w, h)
	}
	if got := imgPixel(t, r, qr, 12, 12); got != "#000000ff" {
		t.Fatalf("qr finder pattern corner = %s", got)
	}
	tight := callImage(t, r, "barcode", imgStr("HI"), imgOpts("format", imgStr("qr"), "scale", imgInt(1), "margin", imgInt(0),
		"color", imgStr("navy"), "background", imgStr("#ffff00")))
	if w, _ := dims(tight); w != 21 {
		t.Fatalf("qr margin 0 scale 1 width = %d, want 21", w)
	}
	if got := imgPixel(t, r, tight, 0, 0); got != "#000080ff" {
		t.Fatalf("qr custom color = %s", got)
	}
	if got := imgPixel(t, r, tight, 1, 1); got != "#ffff00ff" {
		t.Fatalf("qr custom background = %s", got)
	}
	sized := callImage(t, r, "barcode", imgStr("HI"), imgOpts("format", imgStr("qr"), "width", imgInt(200), "height", imgInt(120)))
	if w, h := dims(sized); w != 200 || h != 120 {
		t.Fatalf("qr explicit canvas = %dx%d", w, h)
	}
	ean := callImage(t, r, "barcode", imgStr("5901234123457"), imgOpts("format", imgStr("ean13"), "width", imgInt(300), "height", imgInt(120)))
	if w, h := dims(ean); w != 300 || h != 120 {
		t.Fatalf("ean13 explicit canvas = %dx%d", w, h)
	}

	low := callImage(t, r, "barcode", imgStr(strings.Repeat("A", 40)), imgOpts("format", imgStr("qr"), "level", imgStr("L"), "scale", imgInt(1)))
	high := callImage(t, r, "barcode", imgStr(strings.Repeat("A", 40)), imgOpts("format", imgStr("qr"), "level", imgStr("h"), "scale", imgInt(1)))
	wl, _ := dims(low)
	wh, _ := dims(high)
	if wh <= wl {
		t.Fatalf("level H (%d) should need more modules than level L (%d)", wh, wl)
	}
}

func TestBarcodeErrors(t *testing.T) {
	r := NewBuiltinRegistry()
	bc := func(want, data string, opts runtime.Value) {
		t.Helper()
		_, err := r.Call("imagenative", "barcode", []runtime.Value{imgStr(data), opts})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("barcode(%q) error = %v, want containing %q", data, err, want)
		}
	}
	bc("\"format\" must be one of aztec, codabar", "x", imgOpts())
	bc("\"format\" must be one of", "x", imgOpts("format", imgStr("nope")))
	bc("must not be empty", "", imgOpts("format", imgStr("qr")))
	bc("ean13 data must be 12 digits", "123", imgOpts("format", imgStr("ean13")))
	bc("ean8 data must be 7 digits", "590123412345", imgOpts("format", imgStr("ean8")))
	bc("upca data must be 11 digits", "590123412345777", imgOpts("format", imgStr("upca")))
	bc("barcode.encode: ean13:", "59012341234AB", imgOpts("format", imgStr("ean13")))
	bc("barcode.encode: ean13:", "5901234123450", imgOpts("format", imgStr("ean13")))
	bc("\"level\" must be", "x", imgOpts("format", imgStr("qr"), "level", imgStr("Z")))
	bc("\"level\" must be an int between 0 and 8", "x", imgOpts("format", imgStr("pdf417"), "level", imgInt(9)))
	bc("too small", "Hello", imgOpts("format", imgStr("qr"), "width", imgInt(10)))
	bc("\"scale\" must be an int", "x", imgOpts("format", imgStr("qr"), "scale", imgInt(0)))
	bc("\"checksum\" must be a bool", "X", imgOpts("format", imgStr("code39"), "checksum", imgInt(1)))
	bc("barcode.encode: qr:", strings.Repeat("x", 8000), imgOpts("format", imgStr("qr")))
	bc("invalid color", "x", imgOpts("format", imgStr("qr"), "color", imgStr("nope")))
	bc("exceed 16384 pixels per side", strings.Repeat("x", 500), imgOpts("format", imgStr("qr"), "scale", imgInt(256)))
}
