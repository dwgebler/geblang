package native

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"geblang/internal/runtime"
)

func archiveTestZip(t *testing.T, name string, data []byte, mode os.FileMode) string {
	t.Helper()
	location := filepath.Join(t.TempDir(), "input.zip")
	file, err := os.Create(location)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(mode)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return location
}

func archiveTestEntry(t *testing.T, location string) (*Registry, runtime.Value, runtime.Value) {
	t.Helper()
	registry := NewBuiltinRegistry()
	reader, err := registry.Call("archive_native", "open", []runtime.Value{
		runtime.String{Value: location}, runtime.String{Value: "auto"},
	})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := registry.Call("archive_native", "next", []runtime.Value{reader})
	if err != nil {
		t.Fatal(err)
	}
	dict := metadata.(runtime.Dict)
	key := runtime.String{Value: "handle"}
	handle, ok := dict.GetEntry(DictKey(key))
	if !ok {
		t.Fatal("entry handle missing")
	}
	t.Cleanup(func() {
		_, _ = registry.Call("archive_native", "closeReader", []runtime.Value{reader})
	})
	return registry, reader, handle.Value
}

func TestArchiveStreamRejectsUnsafeEntryNamesAndLinks(t *testing.T) {
	cases := []struct {
		name string
		mode os.FileMode
	}{
		{"../escape.txt", 0o600},
		{"/absolute.txt", 0o600},
		{"C:/drive.txt", 0o600},
		{"link.txt", os.ModeSymlink | 0o777},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			location := archiveTestZip(t, tc.name, []byte("secret"), tc.mode)
			registry, _, entry := archiveTestEntry(t, location)

			_, err := registry.Call("archive_native", "validateEntry", []runtime.Value{entry})

			if err == nil {
				t.Fatal("unsafe entry was accepted")
			}
		})
	}
}

func TestArchiveStreamRejectsTarHardlink(t *testing.T) {
	location := filepath.Join(t.TempDir(), "input.tar")
	file, err := os.Create(location)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	if err := writer.WriteHeader(&tar.Header{Name: "linked", Linkname: "../outside", Typeflag: tar.TypeLink}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	registry, _, entry := archiveTestEntry(t, location)

	_, err = registry.Call("archive_native", "validateEntry", []runtime.Value{entry})

	if err == nil || !strings.Contains(err.Error(), "link") {
		t.Fatalf("expected hardlink rejection, got %v", err)
	}
}

func TestArchiveStreamRejectsExistingDestinationSymlink(t *testing.T) {
	location := archiveTestZip(t, "link/file.txt", []byte("secret"), 0o600)
	registry, _, entry := archiveTestEntry(t, location)
	destination := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(destination, "link")); err != nil {
		t.Fatal(err)
	}

	_, err := registry.Call("archive_native", "extractEntry", []runtime.Value{
		entry, runtime.String{Value: destination}, runtime.Bool{Value: false},
		runtime.NewInt64(1024), runtime.NewInt64(1024),
	})

	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "file.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside file was written: %v", err)
	}
}

func TestArchiveStreamRemovesPartiallyExtractedOversizedFile(t *testing.T) {
	location := archiveTestZip(t, "large.bin", bytes.Repeat([]byte("x"), 131072), 0o600)
	registry, _, entry := archiveTestEntry(t, location)
	destination := t.TempDir()

	_, err := registry.Call("archive_native", "extractEntry", []runtime.Value{
		entry, runtime.String{Value: destination}, runtime.Bool{Value: false},
		runtime.NewInt64(1024), runtime.NewInt64(1024),
	})

	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected size rejection, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "large.bin")); !os.IsNotExist(err) {
		t.Fatalf("partial file remains: %v", err)
	}
}

func TestArchiveStreamOverwriteDoesNotModifyHardlinkedFile(t *testing.T) {
	location := archiveTestZip(t, "item.txt", []byte("replacement"), 0o600)
	registry, _, entry := archiveTestEntry(t, location)
	destination := t.TempDir()
	outside := filepath.Join(t.TempDir(), "original.txt")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(destination, "item.txt")); err != nil {
		t.Fatal(err)
	}

	_, err := registry.Call("archive_native", "extractEntry", []runtime.Value{
		entry, runtime.String{Value: destination}, runtime.Bool{Value: true},
		runtime.NewInt64(1024), runtime.NewInt64(1024),
	})

	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(outside)
	if err != nil || string(original) != "original" {
		t.Fatalf("hardlinked source changed: %q, %v", original, err)
	}
	replaced, err := os.ReadFile(filepath.Join(destination, "item.txt"))
	if err != nil || string(replaced) != "replacement" {
		t.Fatalf("replacement missing: %q, %v", replaced, err)
	}
}

func TestArchiveStreamOpenRejectsMalformedData(t *testing.T) {
	location := filepath.Join(t.TempDir(), "bad.zip")
	if err := os.WriteFile(location, []byte("not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := NewBuiltinRegistry().Call("archive_native", "open", []runtime.Value{
		runtime.String{Value: location}, runtime.String{Value: "auto"},
	})

	if err == nil || !strings.Contains(err.Error(), "unrecognized") {
		t.Fatalf("expected malformed archive rejection, got %v", err)
	}
}
