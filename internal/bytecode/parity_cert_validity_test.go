package bytecode_test

import "testing"

func TestCertNotBeforeParity(t *testing.T) {
	ev, vm := parityOutputsStdlib(t, `import crypt;
import io;

let cert = crypt.generateSelfSignedCert({"subject": {"commonName": "x"}, "notBefore": "2026-01-02T03:04:05Z"});
io.println(crypt.parseCert(cert["cert"])["notBefore"]);
try {
    crypt.generateSelfSignedCert({"subject": {"commonName": "x"}, "notBefore": "2999-01-01T00:00:00Z", "validDays": 1});
} catch (Error e) {
    io.println(e.message);
}
`)

	want := "2026-01-02T03:04:05Z\ncrypt.generateSelfSignedCert notBefore must be before the end of the validity period\n"
	if ev != want || vm != want {
		t.Fatalf("certificate notBefore mismatch\nevaluator: %q\nvm:        %q\nwant:      %q", ev, vm, want)
	}
}
