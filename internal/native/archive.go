package native

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"sort"

	"geblang/internal/runtime"
)

// registerArchive wires the eager archive functions used by the source module.
func registerArchive(r *Registry) {
	r.Register("archive_native", "zipRead", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleBytes(args, "archive.zipRead")
		if err != nil {
			return nil, err
		}
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("archive.zipRead: %w", err)
		}
		entries := make([]runtime.Value, 0, len(reader.File))
		for _, f := range reader.File {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("archive.zipRead: open %s: %w", f.Name, err)
			}
			body, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				return nil, fmt.Errorf("archive.zipRead: read %s: %w", f.Name, err)
			}
			entries = append(entries, archiveEntryDict(f.Name, body, f.FileInfo().IsDir(), int64(f.UncompressedSize64)))
		}
		return &runtime.List{Elements: entries}, nil
	})
	r.Register("archive_native", "zipWrite", func(args []runtime.Value) (runtime.Value, error) {
		entries, err := singleArchiveEntries(args, "archive.zipWrite")
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)
		for _, e := range entries {
			fw, err := w.Create(e.name)
			if err != nil {
				_ = w.Close()
				return nil, fmt.Errorf("archive.zipWrite: create %s: %w", e.name, err)
			}
			if _, err := fw.Write(e.data); err != nil {
				_ = w.Close()
				return nil, fmt.Errorf("archive.zipWrite: write %s: %w", e.name, err)
			}
		}
		if err := w.Close(); err != nil {
			return nil, fmt.Errorf("archive.zipWrite: %w", err)
		}
		return runtime.Bytes{Value: buf.Bytes()}, nil
	})
	r.Register("archive_native", "tarRead", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleBytes(args, "archive.tarRead")
		if err != nil {
			return nil, err
		}
		return readTar(bytes.NewReader(data), "archive.tarRead")
	})
	r.Register("archive_native", "tarWrite", func(args []runtime.Value) (runtime.Value, error) {
		entries, err := singleArchiveEntries(args, "archive.tarWrite")
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := writeTar(&buf, entries); err != nil {
			return nil, fmt.Errorf("archive.tarWrite: %w", err)
		}
		return runtime.Bytes{Value: buf.Bytes()}, nil
	})
	r.Register("archive_native", "tarGzRead", func(args []runtime.Value) (runtime.Value, error) {
		data, err := singleBytes(args, "archive.tarGzRead")
		if err != nil {
			return nil, err
		}
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("archive.tarGzRead: %w", err)
		}
		defer gz.Close()
		return readTar(gz, "archive.tarGzRead")
	})
	r.Register("archive_native", "tarGzWrite", func(args []runtime.Value) (runtime.Value, error) {
		entries, err := singleArchiveEntries(args, "archive.tarGzWrite")
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if err := writeTar(gz, entries); err != nil {
			_ = gz.Close()
			return nil, fmt.Errorf("archive.tarGzWrite: %w", err)
		}
		if err := gz.Close(); err != nil {
			return nil, fmt.Errorf("archive.tarGzWrite: %w", err)
		}
		return runtime.Bytes{Value: buf.Bytes()}, nil
	})
}

type archiveEntry struct {
	name string
	data []byte
}

func archiveEntryDict(name string, data []byte, isDir bool, size int64) runtime.Value {
	entries := map[string]runtime.DictEntry{}
	put := func(key string, value runtime.Value) {
		k := runtime.String{Value: key}
		entries[DictKey(k)] = runtime.DictEntry{Key: k, Value: value}
	}
	put("name", runtime.String{Value: name})
	put("data", runtime.Bytes{Value: data})
	put("isDir", runtime.Bool{Value: isDir})
	put("size", runtime.NewInt64(size))
	return runtime.Dict{Entries: entries}
}

// singleArchiveEntries pulls a list<dict<string, any>> from args[0]
// and normalises each entry into {name, data bytes}.
func singleArchiveEntries(args []runtime.Value, label string) ([]archiveEntry, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s expects exactly one argument", label)
	}
	list, ok := args[0].(*runtime.List)
	if !ok {
		return nil, fmt.Errorf("%s expects a list of entry dicts", label)
	}
	out := make([]archiveEntry, 0, len(list.Elements))
	for i, el := range list.Elements {
		dict, ok := el.(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("%s entry %d must be a dict", label, i)
		}
		name, err := dictStringField(dict, "name")
		if err != nil {
			return nil, fmt.Errorf("%s entry %d: %w", label, i, err)
		}
		data, err := dictDataField(dict, "data")
		if err != nil {
			return nil, fmt.Errorf("%s entry %d: %w", label, i, err)
		}
		out = append(out, archiveEntry{name: name, data: data})
	}
	return out, nil
}

func dictStringField(d runtime.Dict, field string) (string, error) {
	for _, dk := range d.EntryKeys() {
		entry, _ := d.GetEntry(dk)
		key, ok := entry.Key.(runtime.String)
		if !ok || key.Value != field {
			continue
		}
		s, ok := entry.Value.(runtime.String)
		if !ok {
			return "", fmt.Errorf("field %q must be string", field)
		}
		return s.Value, nil
	}
	return "", fmt.Errorf("missing field %q", field)
}

func dictDataField(d runtime.Dict, field string) ([]byte, error) {
	for _, dk := range d.EntryKeys() {
		entry, _ := d.GetEntry(dk)
		key, ok := entry.Key.(runtime.String)
		if !ok || key.Value != field {
			continue
		}
		switch v := entry.Value.(type) {
		case runtime.Bytes:
			return v.Value, nil
		case runtime.String:
			return []byte(v.Value), nil
		default:
			return nil, fmt.Errorf("field %q must be string or bytes", field)
		}
	}
	return nil, fmt.Errorf("missing field %q", field)
}

func readTar(src io.Reader, label string) (runtime.Value, error) {
	r := tar.NewReader(src)
	var entries []runtime.Value
	for {
		hdr, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label, err)
		}
		var body []byte
		isDir := hdr.Typeflag == tar.TypeDir
		if !isDir {
			body, err = io.ReadAll(r)
			if err != nil {
				return nil, fmt.Errorf("%s: read %s: %w", label, hdr.Name, err)
			}
		}
		entries = append(entries, archiveEntryDict(hdr.Name, body, isDir, hdr.Size))
	}
	return &runtime.List{Elements: entries}, nil
}

func writeTar(dst io.Writer, entries []archiveEntry) error {
	w := tar.NewWriter(dst)
	// Deterministic ordering: writers commonly want stable output for
	// tests and content-addressed caches.
	sorted := make([]archiveEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].name < sorted[j].name })
	for _, e := range sorted {
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     0o644,
			Size:     int64(len(e.data)),
			Typeflag: tar.TypeReg,
		}
		if err := w.WriteHeader(hdr); err != nil {
			_ = w.Close()
			return fmt.Errorf("header %s: %w", e.name, err)
		}
		if _, err := w.Write(e.data); err != nil {
			_ = w.Close()
			return fmt.Errorf("write %s: %w", e.name, err)
		}
	}
	return w.Close()
}
