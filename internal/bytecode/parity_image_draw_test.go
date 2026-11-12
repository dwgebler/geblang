package bytecode_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const parityImageDonor = `module canvas_donor;

import image;
import barcode;

export class Badge extends image.Image {
    func Badge(int size) {
        parent(image.blank(size, size, "white")._handle);
        this.rect(0, 0, size, size, {"color": "black"});
    }

    func mark(string color): Badge {
        this.rect(2, 2, 4, 4, {"fill": color});
        return this;
    }
}

export func underline(image.Image img, string color): image.Image {
    return img.line(0, img.height() - 1, img.width() - 1, img.height() - 1, {"color": color});
}

export func code(string data): image.Image {
    return barcode.encode(data, {"format": "code128", "scale": 1});
}
`

func parityImageOutputs(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "canvas_donor.gb"), []byte(parityImageDonor), 0o644); err != nil {
		t.Fatal(err)
	}
	ev, vm := parityOutputsModulesDir(t, dir, source)
	if ev != vm {
		t.Fatalf("output mismatch:\n  evaluator: %q\n  vm:        %q", ev, vm)
	}
	return ev
}

func TestParityImageDrawing(t *testing.T) {
	got := parityImageOutputs(t, `import io;
import image;

let img = image.blank(40, 40, "white");
let same = img.rect(2, 2, 10, 10, {"fill": "#3366ff"})
    .circle(28, 10, 6, {"color": "red", "width": 2})
    .ellipse(10, 30, 8, 4, {"fill": "green"})
    .polygon([[24, 24], [38, 24], [31.5, 38]], {"fill": "#00000080", "color": "black"})
    .arc(20, 20, 5, 0, 180, {"color": "purple", "width": 2})
    .line(0, 39, 39, 39)
    .text("Hi", 14, 14, {"size": 12, "font": "bold", "color": "navy"});
for (p in [[5, 5], [28, 4], [28, 10], [10, 30], [31, 30], [20, 39], [0, 0]]) {
    io.println(img.getPixel(p[0] as int, p[1] as int));
}
same.setPixel(0, 0, "#11223344");
io.println(img.getPixel(0, 0));
io.println(image.measureText("Hello\nWorld", {"size": 18, "font": "mono"}));
io.println(image.rgba(1, 2, 3, a: 4));
let png = img.encode("png");
io.println(png.length());
let back = image.loadBytes(png).rect(...[0, 0, 3, 3], opts: {"fill": "lime"});
io.println(back.getPixel(1, 1) + " " + back.getPixel(5, 5) + " " + back.flipH().getPixel(34, 5));
let snapshot = img.copy();
img.fill("black").paste(snapshot, 20, 20, {"opacity": 0.5});
io.println(img.getPixel(1, 1) + " " + img.getPixel(25, 25) + " " + snapshot.getPixel(25, 25));
`)
	want := "#3366ffff\n#ff0000ff\n#ffffffff\n#008000ff\n"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("unexpected drawing output:\n%s", got)
	}
}

func TestParityImageDrawingCrossModuleInheritance(t *testing.T) {
	got := parityImageOutputs(t, `import io;
import image;
import canvas_donor as donor;

class Star extends donor.Badge {
    func Star() { parent(24); }
    func star(): Star {
        this.polygon([[10, 10], [18, 10], [14, 18]], {"fill": "orange"});
        return this;
    }
}

let b = donor.Badge(12);
b.mark("red").setPixel(11, 11, "blue");
io.println(b.getPixel(0, 5) + " " + b.getPixel(3, 3) + " " + b.getPixel(11, 11) + " " + b.getPixel(8, 8));
let s = Star();
s.star().mark("lime");
donor.underline(s, "teal");
io.println(s.getPixel(14, 12) + " " + s.getPixel(3, 3) + " " + s.getPixel(12, 23) + " " + s.getPixel(0, 12));
io.println("${s instanceof image.Image} ${s.width()} ${s.copy().resize(6, 6).height()}");
let code = donor.code("ABC-123");
io.println("${code.width()}x${code.height()} ${code.getPixel(0, 0)} ${code.getPixel(10, 40)}");
`)
	want := "#000000ff #ff0000ff #0000ffff #ffffffff\n#ffa500ff #00ff00ff #008080ff #000000ff\ntrue 24 6\n"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("unexpected cross-module output:\n%s", got)
	}
}

func TestParityBarcodeEncoding(t *testing.T) {
	got := parityImageOutputs(t, `import io;
import barcode;
import bytes;
import crypt;

io.println(barcode.formats());
let samples = {
    "aztec": "Hello", "codabar": "A1234B", "code128": "Hello-128", "code39": "HELLO 39",
    "code93": "HELLO93", "datamatrix": "Hello", "ean13": "590123412345", "ean8": "9638507",
    "itf": "12345678", "pdf417": "Hello", "qr": "Hello", "upca": "03600029145"
};
for (format in barcode.formats()) {
    let img = barcode.encode(samples[format] as string, {"format": format});
    io.println("${format} ${img.width()}x${img.height()} ${img.encode("png").length()}");
}
let qr = barcode.qr("HI", {"scale": 3, "level": "Q", "color": "navy", "background": "#ffff00"});
io.println("${qr.width()} ${qr.getPixel(0, 0)} ${qr.getPixel(12, 12)}");
let sized = barcode.qr(data: "HI", opts: {"width": 200, "height": 120});
io.println("${sized.width()}x${sized.height()}");
`)
	if !strings.Contains(got, "qr 116x116 ") || !strings.Contains(got, "87 #ffff00ff #000080ff\n200x120\n") {
		t.Fatalf("unexpected barcode output:\n%s", got)
	}
}

func TestParityImageNativeErrorsSurfaceOnBothBackends(t *testing.T) {
	got := parityImageOutputs(t, `import io;
import image;
import barcode;
import canvas_donor as donor;

let img = image.blank(4, 4);
let attempts = [
    func(): void { img.rotate(45); },
    func(): void { img.fill("nope"); },
    func(): void { img.rect(0, 0, 0, 1); },
    func(): void { img.polygon([1, 2, 3]); },
    func(): void { img.text("x", 0, 0, {"font": "comic"}); },
    func(): void { image.loadBytes(img.encode("png")).getPixel(9, 9); },
    func(): void { barcode.encode("x", {"format": "nope"}); },
    func(): void { barcode.encode("123", {"format": "ean13"}); },
    func(): void { donor.code(""); },
    func(): void { donor.Badge(8).mark("nope"); }
];
for (attempt in attempts) {
    try {
        attempt();
        io.println("no error");
    } catch (RuntimeError e) {
        io.println(e.message);
    }
}
`)
	want := `image.rotate: only multiples of 90 are supported
image.fill: invalid color "nope" (use #rgb, #rgba, #rrggbb, #rrggbbaa, or a color name)
image.rect: width and height must be positive
image.polygon: points must be a list of [x, y] pairs
image.text: unknown font "comic" (regular, bold, italic, mono, or a loaded Font)
image.getPixel: (x, y) is outside the image
barcode.encode: option "format" must be one of aztec, codabar, code128, code39, code93, datamatrix, ean13, ean8, itf, pdf417, qr, upca
barcode.encode: ean13: ean13 data must be 12 digits, or 13 with the check digit
barcode.encode: data must not be empty
image.rect: invalid color "nope" (use #rgb, #rgba, #rrggbb, #rrggbbaa, or a color name)
`
	if got != want {
		t.Fatalf("native error text:\n got: %q\nwant: %q", got, want)
	}
}
