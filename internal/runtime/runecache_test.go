package runtime

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"
)

func resetRuneCacheForTest(t *testing.T) {
	t.Helper()
	runeCacheMu.Lock()
	runeCacheMap.Range(func(key, _ any) bool {
		runeCacheMap.Delete(key)
		return true
	})
	runeCacheOrder = nil
	runeCacheBytes = 0
	runeCacheCount = 0
	runeCacheMu.Unlock()
	t.Cleanup(func() {
		runeCacheMu.Lock()
		runeCacheMap.Range(func(key, _ any) bool {
			runeCacheMap.Delete(key)
			return true
		})
		runeCacheOrder = nil
		runeCacheBytes = 0
		runeCacheCount = 0
		runeCacheMu.Unlock()
	})
}

func TestRuneInfoASCII(t *testing.T) {
	s := "hello"
	ri := StringRuneInfo(s)
	if !ri.ascii {
		t.Fatal("expected ascii")
	}
	if got := ri.RuneCount(s); got != 5 {
		t.Fatalf("RuneCount = %d, want 5", got)
	}
	if got := ri.RuneAt(s, 1); got != "e" {
		t.Fatalf("RuneAt(1) = %q, want e", got)
	}
	if got := ri.Substring(s, 1, 4); got != "ell" {
		t.Fatalf("Substring(1,4) = %q, want ell", got)
	}
}

func TestRuneInfoMultiByte(t *testing.T) {
	s := "aé中z" // a=1 byte, é=2, 中=3, z=1; 4 runes
	ri := StringRuneInfo(s)
	if ri.ascii {
		t.Fatal("expected non-ascii")
	}
	if got := ri.RuneCount(s); got != 4 {
		t.Fatalf("RuneCount = %d, want 4", got)
	}
	for i, want := range []string{"a", "é", "中", "z"} {
		if got := ri.RuneAt(s, i); got != want {
			t.Fatalf("RuneAt(%d) = %q, want %q", i, got, want)
		}
	}
	if got := ri.Substring(s, 1, 3); got != "é中" {
		t.Fatalf("Substring(1,3) = %q, want é中", got)
	}
	if got := ri.Substring(s, 0, 4); got != s {
		t.Fatalf("Substring(0,4) = %q, want whole", got)
	}
}

func TestRuneInfoEmpty(t *testing.T) {
	ri := StringRuneInfo("")
	if ri.RuneCount("") != 0 {
		t.Fatal("empty RuneCount should be 0")
	}
}

func TestRuneInfoConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for k := 0; k < 200; k++ {
				s := "abc" + strconv.Itoa((g+k)%50) + "é"
				ri := StringRuneInfo(s)
				_ = ri.RuneCount(s)
				if n := ri.RuneCount(s); n != len([]rune(s)) {
					t.Errorf("RuneCount mismatch for %q", s)
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestRuneCacheEvictionAndGC(t *testing.T) {
	resetRuneCacheForTest(t)
	for k := 0; k < runeCacheCountCap*3; k++ {
		s := strings.Repeat("a", shortStringThreshold+1) + strconv.Itoa(k) + "\u00e9\u4e2d"
		ri := StringRuneInfo(s)
		if ri.RuneCount(s) != len([]rune(s)) {
			t.Fatalf("wrong count for %q", s)
		}
	}
	runeCacheMu.Lock()
	if runeCacheCount > runeCacheCountCap {
		t.Errorf("cache count = %d, cap = %d", runeCacheCount, runeCacheCountCap)
	}
	if runeCacheBytes > runeCacheByteCap {
		t.Errorf("cache bytes = %d, cap = %d", runeCacheBytes, runeCacheByteCap)
	}
	actualCount := 0
	runeCacheMap.Range(func(_, _ any) bool {
		actualCount++
		return true
	})
	if actualCount != runeCacheCount {
		t.Errorf("cache count = %d, map entries = %d", runeCacheCount, actualCount)
	}
	runeCacheMu.Unlock()
	runtime.GC()
	// A fresh string still resolves correctly after eviction + GC.
	s := "after-gc-é中z"
	if StringRuneInfo(s).RuneCount(s) != len([]rune(s)) {
		t.Fatal("post-eviction lookup wrong")
	}
}

func TestRuneCacheThresholdBoundary(t *testing.T) {
	resetRuneCacheForTest(t)
	short := strings.Repeat("a", shortStringThreshold)
	StringRuneInfo(short)
	if runeCacheCount != 0 {
		t.Fatalf("%d-byte string entered cache", shortStringThreshold)
	}

	long := strings.Repeat("a", shortStringThreshold+1)
	StringRuneInfo(long)
	if runeCacheCount != 1 {
		t.Fatalf("%d-byte string did not enter cache", shortStringThreshold+1)
	}
}

func TestRuneCacheAccountsForRetainedASCIIStrings(t *testing.T) {
	resetRuneCacheForTest(t)
	const stringSize = 1 << 20
	for i := 0; i < 8; i++ {
		suffix := strconv.Itoa(i)
		s := strings.Repeat("a", stringSize-len(suffix)) + suffix
		StringRuneInfo(s)
	}

	runeCacheMu.Lock()
	defer runeCacheMu.Unlock()
	if runeCacheBytes > runeCacheByteCap {
		t.Fatalf("cache bytes = %d, cap = %d", runeCacheBytes, runeCacheByteCap)
	}
	if runeCacheCount > 3 {
		t.Fatalf("cache retained %d one-MiB ASCII strings", runeCacheCount)
	}
	runeCacheMap.Range(func(_, cached any) bool {
		entry := cached.(*runeCacheEntry)
		if entry.retainedBytes < len(entry.s)+runeCacheEntryOverhead {
			t.Errorf("entry accounting %d excludes retained source of %d bytes", entry.retainedBytes, len(entry.s))
		}
		return true
	})
}

func TestRuneCacheRejectsOversizedEntry(t *testing.T) {
	resetRuneCacheForTest(t)
	s := strings.Repeat("\u00e9", 750_000)
	info := StringRuneInfo(s)
	if info.RuneCount(s) != 750_000 {
		t.Fatalf("RuneCount = %d, want 750000", info.RuneCount(s))
	}
	if runeCacheCount != 0 {
		t.Fatalf("oversized entry was admitted to cache")
	}
	if runeCacheBytes != 0 {
		t.Fatalf("cache bytes = %d after oversized entry", runeCacheBytes)
	}
}

func TestRuneResultsDoNotRetainSourceBackingArray(t *testing.T) {
	s := strings.Repeat("a", 1024) + "\u00e9\u4e2d"
	info := StringRuneInfo(s)
	sourceStart := uintptr(unsafe.Pointer(unsafe.StringData(s)))
	sourceEnd := sourceStart + uintptr(len(s))

	results := []string{
		info.RuneAt(s, 0),
		info.RuneAt(s, info.RuneCount(s)-1),
		info.Substring(s, 10, 20),
	}
	for _, result := range results {
		resultData := uintptr(unsafe.Pointer(unsafe.StringData(result)))
		if resultData >= sourceStart && resultData < sourceEnd {
			t.Fatalf("result of length %d shares source backing array", len(result))
		}
	}
}

func TestRuneInfoInvalidUTF8(t *testing.T) {
	cases := []string{
		"a\xffb",
		"\xff\xfe",
		"x\xe2\x28\xa1y",             // invalid 3-byte sequence
		strings.Repeat("z\xff", 130), // >256 bytes, hits cached path
	}
	for _, s := range cases {
		ri := StringRuneInfo(s)
		ref := []rune(s)
		if got := ri.RuneCount(s); got != len(ref) {
			t.Errorf("RuneCount(%q) = %d, want %d", s, got, len(ref))
		}
		for i := range ref {
			if got, want := ri.RuneAt(s, i), string(ref[i]); got != want {
				t.Errorf("RuneAt(%q, %d) = %q, want %q", s, i, got, want)
			}
		}
		for i := 0; i < len(ref); i++ {
			for j := i; j <= len(ref); j++ {
				if got, want := ri.Substring(s, i, j), string(ref[i:j]); got != want {
					t.Errorf("Substring(%q, %d, %d) = %q, want %q", s, i, j, got, want)
				}
			}
		}
	}
}

func TestRuneCacheConcurrentCachedPath(t *testing.T) {
	// >256-byte valid non-ASCII string exercises the mutex/map/eviction path.
	base := strings.Repeat("é中", 200) // ~1000 bytes
	ref := []rune(base)
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for k := 0; k < 100; k++ {
				// Alternate between the shared base and distinct strings to drive eviction.
				var s string
				if k%2 == 0 {
					s = base
				} else {
					s = strings.Repeat("ñ日", 130) + strconv.Itoa((g+k)%50)
				}
				ri := StringRuneInfo(s)
				want := []rune(s)
				if ri.RuneCount(s) != len(want) {
					t.Errorf("RuneCount mismatch for len=%d string", len(s))
				}
				if len(want) > 0 {
					mid := len(want) / 2
					if got := ri.RuneAt(s, mid); got != string(want[mid]) {
						t.Errorf("RuneAt mismatch")
					}
					if got := ri.Substring(s, 0, mid); got != string(want[:mid]) {
						t.Errorf("Substring mismatch")
					}
				}
			}
			// Verify shared base still correct at end.
			ri := StringRuneInfo(base)
			if ri.RuneCount(base) != len(ref) {
				t.Errorf("base RuneCount wrong after concurrent access")
			}
		}(g)
	}
	wg.Wait()
}

func TestRuneCacheConcurrentSameMiss(t *testing.T) {
	resetRuneCacheForTest(t)
	s := strings.Repeat("\u00e9\u4e2d", 200)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			info := StringRuneInfo(s)
			if info.RuneCount(s) != 400 {
				t.Errorf("RuneCount = %d, want 400", info.RuneCount(s))
			}
		}()
	}
	close(start)
	wg.Wait()

	runeCacheMu.Lock()
	defer runeCacheMu.Unlock()
	if runeCacheCount != 1 {
		t.Fatalf("concurrent miss admitted %d entries, want 1", runeCacheCount)
	}
}

func BenchmarkRuneCacheParallelSharedString(b *testing.B) {
	s := strings.Repeat("\u00e9\u4e2d", 200)
	StringRuneInfo(s)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			info := StringRuneInfo(s)
			_ = info.RuneCount(s)
		}
	})
}

func BenchmarkRuneCacheParallelStringSet(b *testing.B) {
	inputs := make([]string, 256)
	for i := range inputs {
		inputs[i] = strings.Repeat("\u00f1\u65e5", 130) + strconv.Itoa(i)
		StringRuneInfo(inputs[i])
	}
	var next atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			index := next.Add(1) % uint64(len(inputs))
			s := inputs[index]
			info := StringRuneInfo(s)
			_ = info.RuneCount(s)
		}
	})
}

func BenchmarkBuildRuneInfo(b *testing.B) {
	cases := []struct {
		name  string
		input string
	}{
		{"ascii", strings.Repeat("a", 4096)},
		{"2-byte", strings.Repeat("\u00e9", 2048)},
		{"3-byte", strings.Repeat("\u4e2d", 1365)},
		{"4-byte", strings.Repeat("\U0001f600", 1024)},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				info, indexBytes := buildRuneInfo(tc.input)
				if info.RuneCount(tc.input) == 0 || indexBytes < 0 {
					b.Fatal("invalid rune information")
				}
			}
		})
	}
}

func BenchmarkStringRuneInfoThreshold(b *testing.B) {
	cases := []struct {
		name  string
		input string
	}{
		{"ascii-256", strings.Repeat("a", 256)},
		{"ascii-257", strings.Repeat("a", 257)},
		{"unicode-256", strings.Repeat("\u00e9", 128)},
		{"unicode-258", strings.Repeat("\u00e9", 129)},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			StringRuneInfo(tc.input)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				info := StringRuneInfo(tc.input)
				if info.RuneCount(tc.input) == 0 {
					b.Fatal("invalid rune information")
				}
			}
		})
	}
}

func TestRuneIndexOfByte(t *testing.T) {
	cases := []struct {
		name string
		s    string
	}{
		{"ascii", "hello world"},
		{"multibyte", "héllo wörld wörld"},
		{"multibyte edges", "ééé"},
		{"invalid utf8", "AB\xffCD\xff\xfféX"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ri := StringRuneInfo(tc.s)
			// every rune boundary must map to its []rune index, including len(s)
			runeIdx := 0
			for b := range tc.s {
				if got := ri.RuneIndexOfByte(tc.s, b); got != runeIdx {
					t.Fatalf("%q byte %d: got %d, want %d", tc.s, b, got, runeIdx)
				}
				runeIdx++
			}
			if got := ri.RuneIndexOfByte(tc.s, len(tc.s)); got != len([]rune(tc.s)) {
				t.Fatalf("%q byte %d (end): got %d, want %d", tc.s, len(tc.s), got, len([]rune(tc.s)))
			}
		})
	}
}

func TestRuneIndexOfByteRunesFallback(t *testing.T) {
	// exercise the offsets-less path directly (reserved for over-int32 strings)
	s := "aéb\xffc"
	ri := &RuneInfo{runes: []rune(s)}
	runeIdx := 0
	for b := range s {
		if got := ri.RuneIndexOfByte(s, b); got != runeIdx {
			t.Fatalf("byte %d: got %d, want %d", b, got, runeIdx)
		}
		runeIdx++
	}
	if got := ri.RuneIndexOfByte(s, len(s)); got != len([]rune(s)) {
		t.Fatalf("end: got %d, want %d", got, len([]rune(s)))
	}
}
