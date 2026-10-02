package native

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"geblang/internal/runtime"
)

type streamArchiveReader struct {
	file    *os.File
	zip     *zip.Reader
	tar     *tar.Reader
	gzip    *gzip.Reader
	index   int
	current *streamArchiveEntry
	closed  bool
}

type streamArchiveEntry struct {
	owner  *streamArchiveReader
	reader io.Reader
	closer io.Closer
	name   string
	size   int64
	kind   byte
	closed bool
}

type streamArchiveWriter struct {
	file   *os.File
	zip    *zip.Writer
	tar    *tar.Writer
	gzip   *gzip.Writer
	closed bool
}

func registerArchiveStream(r *Registry) {
	r.Register("archive_native", "open", archiveStreamOpen)
	r.Register("archive_native", "next", archiveStreamNext)
	r.Register("archive_native", "readBytes", archiveStreamReadBytes)
	r.Register("archive_native", "closeEntry", archiveStreamCloseEntry)
	r.Register("archive_native", "closeReader", archiveStreamCloseReader)
	r.Register("archive_native", "create", archiveStreamCreate)
	r.Register("archive_native", "addFile", archiveStreamAddFile)
	r.Register("archive_native", "addDir", archiveStreamAddDir)
	r.Register("archive_native", "closeWriter", archiveStreamCloseWriter)
	r.Register("archive_native", "extractEntry", archiveStreamExtractEntry)
	r.Register("archive_native", "validateEntry", archiveStreamValidateEntry)
}

func archiveString(v runtime.Value, label string) (string, error) {
	s, ok := v.(runtime.String)
	if !ok {
		return "", fmt.Errorf("%s must be a string", label)
	}
	return s.Value, nil
}

func archiveReader(v runtime.Value) (*streamArchiveReader, error) {
	obj, ok := v.(runtime.NativeObject)
	if !ok || obj.Kind != "ArchiveReader" {
		return nil, fmt.Errorf("archive: expected ArchiveReader")
	}
	reader, ok := obj.Payload.(*streamArchiveReader)
	if !ok {
		return nil, fmt.Errorf("archive: invalid reader")
	}
	return reader, nil
}

func archiveCursorEntry(v runtime.Value) (*streamArchiveEntry, error) {
	obj, ok := v.(runtime.NativeObject)
	if !ok || obj.Kind != "ArchiveEntry" {
		return nil, fmt.Errorf("archive: expected ArchiveEntry")
	}
	entry, ok := obj.Payload.(*streamArchiveEntry)
	if !ok {
		return nil, fmt.Errorf("archive: invalid entry")
	}
	return entry, nil
}

func archiveWriter(v runtime.Value) (*streamArchiveWriter, error) {
	obj, ok := v.(runtime.NativeObject)
	if !ok || obj.Kind != "ArchiveWriter" {
		return nil, fmt.Errorf("archive: expected ArchiveWriter")
	}
	writer, ok := obj.Payload.(*streamArchiveWriter)
	if !ok {
		return nil, fmt.Errorf("archive: invalid writer")
	}
	return writer, nil
}

func archiveDetect(file *os.File) (string, error) {
	header := make([]byte, 512)
	n, err := file.ReadAt(header, 0)
	if err != nil && err != io.EOF {
		return "", err
	}
	if n >= 4 && (string(header[:4]) == "PK\x03\x04" || string(header[:4]) == "PK\x05\x06") {
		return "zip", nil
	}
	if n >= 2 && header[0] == 0x1f && header[1] == 0x8b {
		return "tar.gz", nil
	}
	if n >= 262 && string(header[257:262]) == "ustar" {
		return "tar", nil
	}
	return "", fmt.Errorf("archive.open: unrecognized archive format")
}

func archiveStreamOpen(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("archive.open expects path and format")
	}
	filename, err := archiveString(args[0], "archive.open path")
	if err != nil {
		return nil, err
	}
	format, err := archiveString(args[1], "archive.open format")
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("archive.open %q: %w", filename, err)
	}
	reader := &streamArchiveReader{file: file}
	if format == "auto" {
		format, err = archiveDetect(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	switch format {
	case "zip":
		info, statErr := file.Stat()
		if statErr != nil {
			err = statErr
			break
		}
		reader.zip, err = zip.NewReader(file, info.Size())
	case "tar":
		reader.tar = tar.NewReader(file)
	case "tar.gz":
		reader.gzip, err = gzip.NewReader(file)
		if err == nil {
			reader.tar = tar.NewReader(reader.gzip)
		}
	default:
		err = fmt.Errorf("unsupported format %q", format)
	}
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("archive.open %q: %w", filename, err)
	}
	return runtime.NativeObject{Kind: "ArchiveReader", Payload: reader}, nil
}

func (entry *streamArchiveEntry) close() error {
	if entry.closed {
		return nil
	}
	entry.closed = true
	if entry.closer != nil {
		return entry.closer.Close()
	}
	return nil
}

func (reader *streamArchiveReader) close() error {
	if reader.closed {
		return nil
	}
	reader.closed = true
	var first error
	if reader.current != nil {
		first = reader.current.close()
	}
	if reader.gzip != nil {
		if err := reader.gzip.Close(); first == nil {
			first = err
		}
	}
	if err := reader.file.Close(); first == nil {
		first = err
	}
	return first
}

func archiveStreamNext(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("archive.next expects a reader")
	}
	reader, err := archiveReader(args[0])
	if err != nil {
		return nil, err
	}
	if reader.closed {
		return nil, fmt.Errorf("archive.next: reader is closed")
	}
	if reader.current != nil {
		if err := reader.current.close(); err != nil {
			return nil, fmt.Errorf("archive.next: close previous entry: %w", err)
		}
		reader.current = nil
	}
	var entry *streamArchiveEntry
	if reader.zip != nil {
		if reader.index >= len(reader.zip.File) {
			return runtime.Null{}, nil
		}
		file := reader.zip.File[reader.index]
		reader.index++
		kind := byte('f')
		if file.FileInfo().IsDir() {
			kind = 'd'
		} else if !file.FileInfo().Mode().IsRegular() {
			kind = 'l'
		}
		var body io.ReadCloser
		if kind == 'f' {
			body, err = file.Open()
			if err != nil {
				return nil, fmt.Errorf("archive.next %q: %w", file.Name, err)
			}
		}
		entry = &streamArchiveEntry{owner: reader, reader: body, closer: body, name: file.Name, size: int64(file.UncompressedSize64), kind: kind}
	} else {
		header, nextErr := reader.tar.Next()
		if nextErr == io.EOF {
			return runtime.Null{}, nil
		}
		if nextErr != nil {
			return nil, fmt.Errorf("archive.next: %w", nextErr)
		}
		kind := byte('l')
		if header.Typeflag == tar.TypeDir {
			kind = 'd'
		} else if header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeRegA {
			kind = 'f'
		}
		entry = &streamArchiveEntry{owner: reader, reader: reader.tar, name: header.Name, size: header.Size, kind: kind}
	}
	reader.current = entry
	fields := map[string]runtime.DictEntry{}
	put := func(key string, value runtime.Value) {
		k := runtime.String{Value: key}
		fields[DictKey(k)] = runtime.DictEntry{Key: k, Value: value}
	}
	put("name", runtime.String{Value: entry.name})
	put("size", runtime.NewInt64(entry.size))
	put("isDir", runtime.Bool{Value: entry.kind == 'd'})
	put("kind", runtime.String{Value: string(entry.kind)})
	put("handle", runtime.NativeObject{Kind: "ArchiveEntry", Payload: entry})
	return runtime.Dict{Entries: fields}, nil
}

func archiveStreamReadBytes(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("archive.readBytes expects entry and count")
	}
	entry, err := archiveCursorEntry(args[0])
	if err != nil {
		return nil, err
	}
	count, ok := AsInt64(args[1])
	if !ok || count < 0 {
		return nil, fmt.Errorf("archive.readBytes: count must be nonnegative")
	}
	if entry.closed || entry.owner.closed {
		return nil, fmt.Errorf("archive.readBytes: entry is closed")
	}
	if entry.kind != 'f' || count == 0 {
		return runtime.Bytes{Value: []byte{}}, nil
	}
	if count > 65536 {
		count = 65536
	}
	buf := make([]byte, count)
	n, err := io.ReadAtLeast(entry.reader, buf, 1)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("archive.readBytes %q: %w", entry.name, err)
	}
	return runtime.Bytes{Value: buf[:n]}, nil
}

func archiveStreamCloseEntry(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("archive.closeEntry expects entry")
	}
	entry, err := archiveCursorEntry(args[0])
	if err != nil {
		return nil, err
	}
	return runtime.Null{}, entry.close()
}

func archiveStreamCloseReader(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("archive.closeReader expects reader")
	}
	reader, err := archiveReader(args[0])
	if err != nil {
		return nil, err
	}
	return runtime.Null{}, reader.close()
}

func archiveValidName(name string, directory bool) (string, error) {
	clean := name
	if directory {
		clean = strings.TrimSuffix(clean, "/")
	}
	if clean == "" || clean == "." || strings.ContainsRune(clean, 0) ||
		strings.Contains(clean, "\\") || path.IsAbs(clean) || filepath.IsAbs(clean) ||
		path.Clean(clean) != clean ||
		len(clean) >= 2 && ((clean[0] >= 'A' && clean[0] <= 'Z') ||
			(clean[0] >= 'a' && clean[0] <= 'z')) && clean[1] == ':' {
		return "", fmt.Errorf("archive: invalid entry name %q", name)
	}
	for _, component := range strings.Split(clean, "/") {
		if component == ".." {
			return "", fmt.Errorf("archive: invalid entry name %q", name)
		}
	}
	return clean, nil
}

func archiveStreamCreate(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("archive.create expects path and format")
	}
	filename, err := archiveString(args[0], "archive.create path")
	if err != nil {
		return nil, err
	}
	format, err := archiveString(args[1], "archive.create format")
	if err != nil {
		return nil, err
	}
	if format != "zip" && format != "tar" && format != "tar.gz" {
		return nil, fmt.Errorf("archive.create: unsupported format %q", format)
	}
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("archive.create %q: %w", filename, err)
	}
	writer := &streamArchiveWriter{file: file}
	switch format {
	case "zip":
		writer.zip = zip.NewWriter(file)
	case "tar":
		writer.tar = tar.NewWriter(file)
	case "tar.gz":
		writer.gzip = gzip.NewWriter(file)
		writer.tar = tar.NewWriter(writer.gzip)
	}
	return runtime.NativeObject{Kind: "ArchiveWriter", Payload: writer}, nil
}

func (writer *streamArchiveWriter) close() error {
	if writer.closed {
		return nil
	}
	writer.closed = true
	var first error
	if writer.zip != nil {
		first = writer.zip.Close()
	}
	if writer.tar != nil {
		if err := writer.tar.Close(); first == nil {
			first = err
		}
	}
	if writer.gzip != nil {
		if err := writer.gzip.Close(); first == nil {
			first = err
		}
	}
	if err := writer.file.Close(); first == nil {
		first = err
	}
	return first
}

func archiveStreamAddFile(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 3 {
		return nil, fmt.Errorf("archive.addFile expects writer, name, and source")
	}
	writer, err := archiveWriter(args[0])
	if err != nil {
		return nil, err
	}
	if writer.closed {
		return nil, fmt.Errorf("archive.addFile: writer is closed")
	}
	name, err := archiveString(args[1], "archive.addFile name")
	if err != nil {
		return nil, err
	}
	name, err = archiveValidName(name, false)
	if err != nil {
		return nil, err
	}
	var source io.Reader
	var size int64
	if data, ok := args[2].(runtime.Bytes); ok {
		source = strings.NewReader(string(data.Value))
		size = int64(len(data.Value))
	} else {
		filename, err := archiveString(args[2], "archive.addFile source")
		if err != nil {
			return nil, err
		}
		file, err := os.Open(filename)
		if err != nil {
			return nil, fmt.Errorf("archive.addFile %q: %w", filename, err)
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return nil, fmt.Errorf("archive.addFile %q: %w", filename, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("archive.addFile %q: source is not a regular file", filename)
		}
		source, size = file, info.Size()
	}
	var dest io.Writer
	if writer.zip != nil {
		dest, err = writer.zip.Create(name)
	} else {
		err = writer.tar.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: size, Typeflag: tar.TypeReg})
		if err == nil {
			dest = writer.tar
		}
	}
	if err != nil {
		return nil, fmt.Errorf("archive.addFile %q: %w", name, err)
	}
	n, err := io.Copy(dest, source)
	if err != nil {
		return nil, fmt.Errorf("archive.addFile %q: %w", name, err)
	}
	if n != size {
		return nil, fmt.Errorf("archive.addFile %q: source changed during copy", name)
	}
	return runtime.Null{}, nil
}

func archiveStreamAddDir(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("archive.addDir expects writer and name")
	}
	writer, err := archiveWriter(args[0])
	if err != nil {
		return nil, err
	}
	if writer.closed {
		return nil, fmt.Errorf("archive.addDir: writer is closed")
	}
	name, err := archiveString(args[1], "archive.addDir name")
	if err != nil {
		return nil, err
	}
	name, err = archiveValidName(name, true)
	if err != nil {
		return nil, err
	}
	if writer.zip != nil {
		_, err = writer.zip.Create(name + "/")
	} else {
		err = writer.tar.WriteHeader(&tar.Header{Name: name + "/", Mode: 0o700, Typeflag: tar.TypeDir})
	}
	if err != nil {
		return nil, fmt.Errorf("archive.addDir %q: %w", name, err)
	}
	return runtime.Null{}, nil
}

func archiveStreamCloseWriter(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("archive.closeWriter expects writer")
	}
	writer, err := archiveWriter(args[0])
	if err != nil {
		return nil, err
	}
	if err := writer.close(); err != nil {
		return nil, fmt.Errorf("archive.closeWriter: %w", err)
	}
	return runtime.Null{}, nil
}

func archiveSafeTarget(destination, name string) (string, error) {
	root, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	current := root
	parts := strings.Split(name, "/")
	for _, component := range parts[:len(parts)-1] {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
				return "", err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("archive.extract: %q traverses a non-directory or symlink", name)
		}
	}
	target := filepath.Join(current, parts[len(parts)-1])
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("archive.extract: %q targets a symlink", name)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return target, nil
}

func archiveStreamExtractEntry(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 5 {
		return nil, fmt.Errorf("archive.extractEntry expects entry, destination, overwrite, entry limit, and remaining limit")
	}
	entry, err := archiveCursorEntry(args[0])
	if err != nil {
		return nil, err
	}
	destination, err := archiveString(args[1], "archive.extract destination")
	if err != nil {
		return nil, err
	}
	overwrite, ok := args[2].(runtime.Bool)
	if !ok {
		return nil, fmt.Errorf("archive.extract: overwrite must be bool")
	}
	entryLimit, ok1 := AsInt64(args[3])
	remaining, ok2 := AsInt64(args[4])
	if !ok1 || !ok2 || entryLimit <= 0 || remaining < 0 {
		return nil, fmt.Errorf("archive.extract: invalid size limit")
	}
	if entry.closed || entry.owner.closed {
		return nil, fmt.Errorf("archive.extract: entry is closed")
	}
	name, err := archiveValidName(entry.name, entry.kind == 'd')
	if err != nil {
		return nil, err
	}
	if entry.kind != 'f' && entry.kind != 'd' {
		return nil, fmt.Errorf("archive.extract: %q is a link or unsupported entry", entry.name)
	}
	target, err := archiveSafeTarget(destination, name)
	if err != nil {
		return nil, fmt.Errorf("archive.extract %q: %w", entry.name, err)
	}
	if entry.kind == 'd' {
		if err := os.Mkdir(target, 0o700); err != nil && !os.IsExist(err) {
			return nil, fmt.Errorf("archive.extract %q: %w", entry.name, err)
		}
		info, err := os.Lstat(target)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("archive.extract %q: target is not a directory", entry.name)
		}
		return runtime.NewInt64(0), nil
	}
	var file *os.File
	writePath := target
	if overwrite.Value {
		file, err = os.CreateTemp(filepath.Dir(target), ".archive-extract-*")
		if err == nil {
			writePath = file.Name()
		}
	} else {
		file, err = os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}
	if err != nil {
		return nil, fmt.Errorf("archive.extract %q: %w", entry.name, err)
	}
	completed := false
	defer func() {
		_ = file.Close()
		if !completed {
			_ = os.Remove(writePath)
		}
	}()
	limit := entryLimit
	if remaining < limit {
		limit = remaining
	}
	count, err := io.Copy(file, io.LimitReader(entry.reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("archive.extract %q: %w", entry.name, err)
	}
	if count > limit {
		return nil, fmt.Errorf("archive.extract %q: decompressed size limit exceeded", entry.name)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("archive.extract %q: %w", entry.name, err)
	}
	if overwrite.Value {
		if err := os.Rename(writePath, target); err != nil {
			return nil, fmt.Errorf("archive.extract %q: %w", entry.name, err)
		}
	}
	completed = true
	return runtime.NewInt64(count), nil
}

func archiveStreamValidateEntry(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("archive.validateEntry expects entry")
	}
	entry, err := archiveCursorEntry(args[0])
	if err != nil {
		return nil, err
	}
	if _, err := archiveValidName(entry.name, entry.kind == 'd'); err != nil {
		return nil, err
	}
	if entry.kind != 'f' && entry.kind != 'd' {
		return nil, fmt.Errorf("archive.extract: %q is a link or unsupported entry", entry.name)
	}
	return runtime.Null{}, nil
}
