package dap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportedInterfaceDefaultsKeepTheirSourceAfterFolding(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "main.gb")
	donor := filepath.Join(dir, "donor.gb")
	if err := os.WriteFile(donor, []byte("module donor;\nexport interface HasAnswer {\n    func answer(): int {\n        return 7;\n    }\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(main, []byte("import io;\nimport donor;\nclass Local implements donor.HasAnswer {}\nclass Child extends Local {}\nfunc host(): void { io.println(Child().answer()); }\nhost();\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session := newTestSession(t, main)
	defer session.close()
	session.setBreakpoints(donor, 4)

	session.configurationDone()
	stopped := session.waitStopped()
	frames := session.stackTrace(stopped.ThreadID)
	if frames[0]["source"].(map[string]any)["path"] != donor {
		t.Fatalf("interface default points to consumer file: %#v", frames[0])
	}
	session.continueAll()
	session.waitTerminated()

	if session.collectedOutput() != "7\n" {
		t.Fatalf("output = %q", session.collectedOutput())
	}
}
