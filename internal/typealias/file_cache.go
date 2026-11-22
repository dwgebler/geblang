package typealias

import (
	"crypto/sha256"
	"os"
	"sync"

	"geblang/internal/lexer"
	"geblang/internal/parser"
)

type fileAliasKey struct {
	path      string
	canonical string
}

type fileAliasEntry struct {
	sum     [32]byte
	aliases map[string]string
	clean   bool
}

var (
	fileAliasMu    sync.Mutex
	fileAliasCache = map[fileAliasKey]fileAliasEntry{}
)

// ExportedFromFile returns the aliases a module file exports and the file's SHA-256, reparsing it only when its content changed; clean is false when the file has parse errors. Callers must not modify the returned map.
func ExportedFromFile(path, canonical string) (aliases map[string]string, sum [32]byte, clean bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, sum, false, err
	}
	key := fileAliasKey{path, canonical}
	sum = sha256.Sum256(data)
	fileAliasMu.Lock()
	entry, ok := fileAliasCache[key]
	fileAliasMu.Unlock()
	if ok && entry.sum == sum {
		return entry.aliases, sum, entry.clean, nil
	}
	p := parser.New(lexer.New(string(data)))
	program := p.ParseProgram()
	entry = fileAliasEntry{sum: sum, aliases: ExportedFromProgram(program, canonical), clean: len(p.Errors()) == 0}
	fileAliasMu.Lock()
	fileAliasCache[key] = entry
	fileAliasMu.Unlock()
	return entry.aliases, sum, entry.clean, nil
}

// SourceRecorder is told about each module a lookup consults: its resolved path ("" when it did not resolve) and content hash.
type SourceRecorder func(canonical, path string, sum [32]byte)

// NewFileLookup builds a lookup that reads exported aliases through resolve's file paths and the shared per-file cache; record may be nil.
func NewFileLookup(resolve func(canonical string) (string, bool), requireClean bool, record SourceRecorder) *SourceLookup {
	return &SourceLookup{cache: map[string]map[string]string{}, aliases: func(canonical string) (map[string]string, bool) {
		path, ok := resolve(canonical)
		if !ok {
			if record != nil {
				record(canonical, "", [32]byte{})
			}
			return nil, false
		}
		aliases, sum, clean, err := ExportedFromFile(path, canonical)
		if record != nil {
			record(canonical, path, sum)
		}
		if err != nil || (requireClean && !clean) {
			return nil, false
		}
		return aliases, true
	}}
}
