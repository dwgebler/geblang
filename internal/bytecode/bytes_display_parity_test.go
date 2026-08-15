package bytecode_test

import "testing"

// Interpolation must use display (hex) semantics, not `as string` UTF-8 decode.
func TestParityBytesInterpolationValidUtf8(t *testing.T) {
	runParity(t, `import bytes;
import io;
let b = bytes.fromHex("68656c6c6f");
io.println("bytes: ${b}");
`, "bytes: 68656c6c6f\n")
}

// Non-UTF-8 bytes: OpCast "string" used to error here; display must not.
func TestParityBytesInterpolationNonUtf8(t *testing.T) {
	runParity(t, `import bytes;
import io;
let b = bytes.fromHex("ff00ff");
io.println("bytes: ${b}");
`, "bytes: ff00ff\n")
}

// Format-spec interpolation sweep pin: already hex on both backends.
func TestParityBytesFormatSpecInterpolation(t *testing.T) {
	runParity(t, `import bytes;
import io;
let b = bytes.fromHex("ff00ff");
io.println("${b:>8}");
`, "  ff00ff\n")
}

// io.println(bytes) sweep pin: already hex on both backends.
func TestParityBytesPrintlnDirect(t *testing.T) {
	runParity(t, `import bytes;
import io;
let b = bytes.fromHex("ff00ff");
io.println(b);
`, "ff00ff\n")
}

// Multi-part interpolation exercises the OpAdd chain around the display opcode.
func TestParityBytesInterpolationMultiPart(t *testing.T) {
	runParity(t, `import bytes;
import io;
let a = bytes.fromHex("aa");
let b = bytes.fromHex("bb");
io.println("${a}-${b}");
`, "aa-bb\n")
}
