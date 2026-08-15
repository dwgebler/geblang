package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"geblang/internal/ast"
	"geblang/internal/lexer"
	"geblang/internal/modules"
	"geblang/internal/parser"
)

const Magic = "GEBX"
const TrailerSize = 12 // 8-byte uint64 zip size + 4-byte magic

// ModuleRecord describes one source file in the bundle.
type ModuleRecord struct {
	Canonical  string `json:"canonical"`
	SourcePath string `json:"sourcePath"` // path within the zip archive
	SourceHash string `json:"sourceHash"` // hex-encoded SHA-256 of source bytes
	IsStdlib   bool   `json:"isStdlib"`
}

// Manifest is stored as BUNDLE.json inside the zip.
type Manifest struct {
	Version    string `json:"version"`
	Entry      string `json:"entry"`
	Name       string `json:"name,omitempty"`
	AppVersion string `json:"appVersion,omitempty"`
	// EntryMainArgs: entry main takes args, so the launcher calls main(sys.args()).
	EntryMainArgs bool           `json:"entryMainArgs,omitempty"`
	Modules       []ModuleRecord `json:"modules"`
	Permissions   *Permissions   `json:"permissions,omitempty"`
}

// Capabilities baked into a built binary so it runs without launch flags; nil = default-deny.
type Permissions struct {
	FFI            []string `json:"ffi,omitempty"`
	Onnx           bool     `json:"onnx,omitempty"`
	ProcessControl bool     `json:"processControl,omitempty"`
	Browser        bool     `json:"browser,omitempty"`
}

// Bundle holds the decoded bundle data and its raw zip bytes.
type Bundle struct {
	Manifest Manifest
	data     []byte // raw zip bytes
}

// Hash returns the hex-encoded SHA-256 of the bundle's zip data.
func (b *Bundle) Hash() string {
	h := sha256.Sum256(b.data)
	return hex.EncodeToString(h[:])
}

// OpenFromExecutable inspects the current executable for an appended bundle.
// Returns nil, nil if no bundle trailer is present.
func OpenFromExecutable() (*Bundle, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(exe)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := fi.Size()
	if fileSize < int64(TrailerSize) {
		return nil, nil
	}

	trailer := make([]byte, TrailerSize)
	if _, err := f.ReadAt(trailer, fileSize-int64(TrailerSize)); err != nil {
		return nil, err
	}
	if string(trailer[8:12]) != Magic {
		return nil, nil
	}

	zipSize := int64(binary.LittleEndian.Uint64(trailer[:8]))
	if zipSize <= 0 || zipSize > fileSize-int64(TrailerSize) {
		return nil, fmt.Errorf("bundle: invalid zip size %d in trailer", zipSize)
	}

	zipStart := fileSize - int64(TrailerSize) - zipSize
	data := make([]byte, zipSize)
	if _, err := f.ReadAt(data, zipStart); err != nil {
		return nil, fmt.Errorf("bundle: read zip data: %w", err)
	}

	b, err := parseZip(data)
	if err != nil {
		return nil, fmt.Errorf("bundle: parse zip: %w", err)
	}
	return b, nil
}

func parseZip(data []byte) (*Bundle, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}

	var manifest Manifest
	for _, f := range zr.File {
		if f.Name != "BUNDLE.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			return nil, fmt.Errorf("bundle: parse BUNDLE.json: %w", err)
		}
		break
	}
	if manifest.Entry == "" {
		return nil, fmt.Errorf("bundle: BUNDLE.json missing or has no entry")
	}

	return &Bundle{Manifest: manifest, data: data}, nil
}

// Write serialises files into a zip archive and appends the 12-byte trailer to w.
// files maps zip-internal path → file bytes.
func Write(w io.Writer, manifest Manifest, files map[string][]byte) error {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Write BUNDLE.json first
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	f, err := zw.Create("BUNDLE.json")
	if err != nil {
		return err
	}
	if _, err := f.Write(manifestBytes); err != nil {
		return err
	}

	for name, data := range files {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := f.Write(data); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}

	zipData := buf.Bytes()
	if _, err := w.Write(zipData); err != nil {
		return err
	}

	// Write trailer: uint64 LE zip size + magic
	trailer := make([]byte, TrailerSize)
	binary.LittleEndian.PutUint64(trailer[:8], uint64(len(zipData)))
	copy(trailer[8:], Magic)
	_, err = w.Write(trailer)
	return err
}

// ExtractTo atomically extracts the bundle into dir (temp dir + rename, so an interrupt leaves no partial dir to be reused) and pre-populates the bytecode cache via cachePathFor (nil skips seeding); skips if dir exists.
func (b *Bundle) ExtractTo(dir string, cachePathFor func(sourcePath string, source []byte) string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil // already extracted
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("bundle extract: create parent: %w", err)
	}
	tmpDir, err := os.MkdirTemp(parent, ".geblang-extract-*")
	if err != nil {
		return fmt.Errorf("bundle extract: temp dir: %w", err)
	}
	published := false
	defer func() {
		if !published {
			os.RemoveAll(tmpDir)
		}
	}()

	zr, err := zip.NewReader(bytes.NewReader(b.data), int64(len(b.data)))
	if err != nil {
		return fmt.Errorf("bundle extract: open zip: %w", err)
	}

	// Collect source + bytecode bytes so we can compute the exact bytecode cache key.
	sourceBytesMap := map[string][]byte{}   // zip path -> source bytes
	bytecodeBytesMap := map[string][]byte{} // zip path -> bytecode bytes

	for _, f := range zr.File {
		if f.Name == "BUNDLE.json" {
			continue
		}
		destPath := filepath.Join(tmpDir, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf("bundle extract: mkdir %s: %w", destPath, err)
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("bundle extract: open %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return fmt.Errorf("bundle extract: read %s: %w", f.Name, err)
		}
		if err := os.WriteFile(destPath, data, 0o644); err != nil {
			return fmt.Errorf("bundle extract: write %s: %w", destPath, err)
		}

		if strings.HasSuffix(f.Name, ".gb") {
			sourceBytesMap[f.Name] = data
		} else if strings.HasSuffix(f.Name, ".gbc") && cachePathFor != nil {
			bytecodeBytesMap[f.Name] = data
		}
	}

	// Cache keys use the final dir path (where the source resolves at runtime), not the temp dir.
	for gbcZipPath, bytecodeData := range bytecodeBytesMap {
		gbZipPath := strings.TrimSuffix(gbcZipPath, ".gbc") + ".gb"
		srcBytes, ok := sourceBytesMap[gbZipPath]
		if !ok {
			continue
		}
		finalSrcPath := filepath.Join(dir, filepath.FromSlash(gbZipPath))
		cachePath := cachePathFor(finalSrcPath, srcBytes)
		_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
		_ = os.WriteFile(cachePath, bytecodeData, 0o644)
	}

	if err := os.Rename(tmpDir, dir); err != nil {
		if _, statErr := os.Stat(dir); statErr == nil {
			return nil // another process published it first
		}
		return fmt.Errorf("bundle extract: publish: %w", err)
	}
	published = true
	return nil
}

// WalkImports traverses the import graph starting at entryPath and returns a
// map from canonical module name to absolute file path for every non-native
// module reachable from the entry.
//
// entryCanonical is the canonical name of the entry module itself.
// isNative should return true for built-in modules that need not be bundled.
func WalkImports(
	entryCanonical string,
	entryPath string,
	resolver *modules.Resolver,
	isNative func(string) bool,
) (map[string]string, error) {
	result := map[string]string{}
	visited := map[string]bool{} // keyed by abs path

	var walk func(path string) error
	walk = func(path string) error {
		absPath, err := filepath.Abs(path)
		if err != nil {
			absPath = filepath.Clean(path)
		}
		if visited[absPath] {
			return nil
		}
		visited[absPath] = true

		src, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("bundle: read %s: %w", absPath, err)
		}
		program := parser.New(lexer.New(string(src))).ParseProgram()

		for _, stmt := range program.Statements {
			imp, ok := stmt.(*ast.ImportStatement)
			if !ok {
				continue
			}
			canonical := strings.Join(imp.Path, ".")
			if _, seen := result[canonical]; seen {
				continue
			}
			// Bundle a source-backed module even when also natively registered;
			// only modules with no resolvable source are pure-native.
			depPath, err := resolver.Resolve(canonical)
			if err != nil {
				if isNative(canonical) {
					continue
				}
				return fmt.Errorf("bundle: resolve import %q: %w", canonical, err)
			}
			absDepPath, _ := filepath.Abs(depPath)
			result[canonical] = absDepPath
			if err := walk(absDepPath); err != nil {
				return err
			}
		}
		return nil
	}

	absEntry, err := filepath.Abs(entryPath)
	if err != nil {
		absEntry = filepath.Clean(entryPath)
	}
	result[entryCanonical] = absEntry
	if err := walk(absEntry); err != nil {
		return nil, err
	}
	return result, nil
}

// SourceHash returns the hex-encoded SHA-256 of src.
func SourceHash(src []byte) string {
	h := sha256.Sum256(src)
	return hex.EncodeToString(h[:])
}
