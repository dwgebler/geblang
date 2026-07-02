package bytecode_test

import "testing"

// path.real resolves symlinks identically on both backends via the shared stateful-native bridge.
func TestParityPathReal(t *testing.T) {
	prog := `
import io;
import path;
let dir = io.tempDir("geb-parity-real-*");
let target = path.join(dir, "target.txt");
io.writeText(target, "x");
let link = path.join(dir, "link.txt");
io.symlink(target, link);
io.println((path.real(link) == path.real(target)) as string);
io.println(path.real(target).startsWith("/") as string);
`
	runParityStateful(t, prog, "true\ntrue\n")
}
