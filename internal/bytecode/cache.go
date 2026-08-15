package bytecode

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
)

func CachePath(cacheDir string, sourcePath string, source []byte, compiler string) string {
	return cachePathForVersion(cacheDir, sourcePath, source, compiler, Version)
}

func cachePathForVersion(cacheDir string, sourcePath string, source []byte, compiler string, chunkVersion uint16) string {
	key := []byte(compiler + "\x00" + strconv.FormatUint(uint64(chunkVersion), 10) + "\x00" + sourcePath + "\x00")
	hash := SourceHash(append(key, source...))
	return filepath.Join(cacheDir, hex.EncodeToString(hash[:])+".gbc")
}

// EmbedsFresh reports whether every embedded file recorded in chunk still matches its compile-time hash.
func EmbedsFresh(chunk Chunk, sourcePath string) bool {
	dir := filepath.Dir(sourcePath)
	for _, rec := range chunk.Embeds {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rec.Path)))
		if err != nil || SourceHash(data) != rec.Hash {
			return false
		}
	}
	return true
}
