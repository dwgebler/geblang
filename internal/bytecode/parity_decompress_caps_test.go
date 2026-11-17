package bytecode_test

import "testing"

// gunzip, zipRead and tarGzRead decompressed without any output limit.
func TestParityDecompressionCaps(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import io;
import compress;
import archive;
let gz = compress.gzip("z".repeat(100000) as bytes);
try { compress.gunzip(gz, 99999); io.println("returned"); } catch (IOError e) { io.println("gunzip: " + e.message); }
io.println("unlimited: ${compress.gunzip(gz, 0).length()} default: ${compress.gunzip(gz).length()}");
io.println("fits: ${compress.gunzip(compress.gzip("hello" as bytes), 5) as string}");
let entries = [{"name": "a.txt", "data": "a".repeat(600) as bytes}, {"name": "b.txt", "data": "b".repeat(600) as bytes}];
let z = archive.zipWrite(entries);
let t = archive.tarGzWrite(entries);
try { archive.zipRead(z, 1000); } catch (IOError e) { io.println("zip total: " + e.message); }
try { archive.tarGzRead(t, 1000); } catch (IOError e) { io.println("tgz total: " + e.message); }
io.println("zip fits: ${archive.zipRead(z, 1200).length()} tgz unlimited: ${archive.tarGzRead(t, 0).length()}");
`)
	want := "gunzip: compress.gunzip: decompressed data exceeds 99999 bytes\nunlimited: 100000 default: 100000\nfits: hello\nzip total: archive.zipRead: read b.txt: decompressed data exceeds 1000 bytes\ntgz total: archive.tarGzRead: read b.txt: decompressed data exceeds 1000 bytes\nzip fits: 2 tgz unlimited: 2\n"
	if ev != want || vm != want {
		t.Fatalf("evaluator=%q VM=%q want %q", ev, vm, want)
	}
}
