package runtime

import (
	"math"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
	"unsafe"
)

// RuneInfo holds a string's rune<->byte mapping; ascii strings share asciiRuneInfo.
type RuneInfo struct {
	ascii   bool
	offsets []int32
	runes   []rune // fallback for invalid UTF-8 or strings too large for int32 offsets
}

func (ri *RuneInfo) RuneCount(s string) int {
	if ri.runes != nil {
		return len(ri.runes)
	}
	if ri.ascii {
		return len(s)
	}
	return len(ri.offsets) - 1
}

// RuneAt returns rune i as a string. Caller guarantees 0 <= i < RuneCount.
func (ri *RuneInfo) RuneAt(s string, i int) string {
	if ri.runes != nil {
		return string(ri.runes[i])
	}
	if ri.ascii {
		return strings.Clone(s[i : i+1])
	}
	return strings.Clone(s[ri.offsets[i]:ri.offsets[i+1]])
}

// RuneIndexOfByte maps a byte offset on a rune boundary to its rune index.
func (ri *RuneInfo) RuneIndexOfByte(s string, b int) int {
	if ri.offsets != nil {
		return sort.Search(len(ri.offsets), func(i int) bool { return int(ri.offsets[i]) >= b })
	}
	if ri.ascii {
		return b
	}
	// offsets-less fallback (only strings too large for int32 offsets); linear prefix count.
	return utf8.RuneCountInString(s[:b])
}

// Substring returns runes [i,j). Caller guarantees 0 <= i <= j <= RuneCount.
func (ri *RuneInfo) Substring(s string, i, j int) string {
	if ri.runes != nil {
		return string(ri.runes[i:j])
	}
	if ri.ascii {
		return strings.Clone(s[i:j])
	}
	return strings.Clone(s[ri.offsets[i]:ri.offsets[j]])
}

type runeCacheKey struct {
	data uintptr
	n    int
}

type runeCacheEntry struct {
	s             string // strong ref pins backing array; entry.s==s guards against key reuse
	info          *RuneInfo
	retainedBytes int
}

const (
	runeCacheCountCap      = 4096
	runeCacheByteCap       = 4 << 20
	shortStringThreshold   = 256 // strings at or below this byte length skip the lock+map
	runeCacheEntryOverhead = int(unsafe.Sizeof(runeCacheEntry{})) +
		int(unsafe.Sizeof(runeCacheKey{}))
)

var (
	runeCacheMu    sync.Mutex
	runeCacheMap   sync.Map
	runeCacheOrder []runeCacheKey
	runeCacheBytes int
	runeCacheCount int
	asciiRuneInfo  = &RuneInfo{ascii: true}
)

// StringRuneInfo returns cached rune<->byte mapping for s, building it on a miss.
func StringRuneInfo(s string) *RuneInfo {
	if len(s) == 0 {
		return asciiRuneInfo
	}
	// Short strings: O(n) scan is cheaper than lock+map per call.
	if len(s) <= shortStringThreshold {
		info, _ := buildRuneInfo(s)
		return info
	}
	if len(s) > runeCacheByteCap-runeCacheEntryOverhead {
		info, _ := buildRuneInfo(s)
		return info
	}
	key := runeCacheKey{uintptr(unsafe.Pointer(unsafe.StringData(s))), len(s)}
	if cached, ok := runeCacheMap.Load(key); ok && cached.(*runeCacheEntry).s == s {
		return cached.(*runeCacheEntry).info
	}
	info, indexBytes := buildRuneInfo(s)
	retainedBytes := len(s) + indexBytes + runeCacheEntryOverhead
	if retainedBytes > runeCacheByteCap {
		return info
	}

	runeCacheMu.Lock()
	if cached, ok := runeCacheMap.Load(key); ok && cached.(*runeCacheEntry).s == s {
		runeCacheMu.Unlock()
		return cached.(*runeCacheEntry).info
	}
	entry := &runeCacheEntry{s: s, info: info, retainedBytes: retainedBytes}
	runeCacheMap.Store(key, entry)
	runeCacheOrder = append(runeCacheOrder, key)
	runeCacheBytes += retainedBytes
	runeCacheCount++
	for (runeCacheBytes > runeCacheByteCap || runeCacheCount > runeCacheCountCap) && len(runeCacheOrder) > 0 {
		old := runeCacheOrder[0]
		runeCacheOrder = runeCacheOrder[1:]
		if cached, ok := runeCacheMap.LoadAndDelete(old); ok {
			runeCacheBytes -= cached.(*runeCacheEntry).retainedBytes
			runeCacheCount--
		}
	}
	runeCacheMu.Unlock()
	return info
}

func buildRuneInfo(s string) (*RuneInfo, int) {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			if len(s) > math.MaxInt32 {
				runes := []rune(s)
				return &RuneInfo{runes: runes}, cap(runes) * int(unsafe.Sizeof(rune(0)))
			}
			runeCount, valid := validRuneCount(s)
			if !valid {
				// invalid UTF-8: match old string([]rune(s)) semantics (U+FFFD replacement)
				runes := []rune(s)
				// offsets too: range advances 1 byte per invalid byte, matching []rune(s) element-for-element.
				offsets := make([]int32, 0, len(runes)+1)
				for b := range s {
					offsets = append(offsets, int32(b))
				}
				offsets = append(offsets, int32(len(s)))
				return &RuneInfo{runes: runes, offsets: offsets},
					cap(runes)*int(unsafe.Sizeof(rune(0))) + cap(offsets)*int(unsafe.Sizeof(int32(0)))
			}
			offsets := make([]int32, 0, runeCount+1)
			for b := range s {
				offsets = append(offsets, int32(b))
			}
			offsets = append(offsets, int32(len(s)))
			return &RuneInfo{offsets: offsets}, cap(offsets) * int(unsafe.Sizeof(int32(0)))
		}
	}
	return asciiRuneInfo, 0
}

func validRuneCount(s string) (int, bool) {
	count := 0
	for len(s) > 0 {
		_, size := utf8.DecodeRuneInString(s)
		if size == 1 && s[0] >= utf8.RuneSelf {
			return 0, false
		}
		s = s[size:]
		count++
	}
	return count, true
}
