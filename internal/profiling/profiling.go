// Package profiling samples Geblang call stacks at runtime safepoints.
package profiling

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Interval is the sampling period.
const Interval = time.Millisecond

// Active is atomic because cancelled workers can outlive a collection.
var Active atomic.Bool

var (
	tick       atomic.Uint64
	mu         sync.Mutex
	stacks     map[string]int
	lines      map[string]int
	samples    int
	path       string
	started    time.Time
	stop       chan struct{}
	done       sync.Once
	threadTick uint64
	threads    map[int64]struct{}
)

// Start turns profiling on, writing the report to reportPath when Finish
// runs. It must be called before any Geblang code starts.
func Start(reportPath string) {
	mu.Lock()
	defer mu.Unlock()
	if Active.Load() {
		return
	}
	path = reportPath
	samples = 0
	stacks = map[string]int{}
	lines = map[string]int{}
	threads = map[int64]struct{}{}
	started = time.Now()
	stopped := make(chan struct{})
	stop = stopped
	Active.Store(true)
	go func() {
		t := time.NewTicker(Interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				tick.Add(1)
			case <-stopped:
				return
			}
		}
	}()
}

// Collect turns sampling on without a report and returns a function that
// turns it off again and returns the folded stacks gathered meanwhile. For
// tests, which run programs one after another in one process.
func Collect() func() map[string]int {
	Start("")
	return func() map[string]int {
		close(stop)
		Active.Store(false)
		mu.Lock()
		defer mu.Unlock()
		out := stacks
		stacks, lines, samples = nil, nil, 0
		done = sync.Once{}
		return out
	}
}

// Tick is the current sampling tick; a safepoint samples when it differs
// from the tick it last sampled at.
func Tick() uint64 { return tick.Load() }

// Record adds one sample: frames is the call stack, outermost first, as
// "module.function" names, and leaf is the innermost function's
// "module.function:line" (empty when the line is not known).
func Record(frames []string, leaf string) {
	mu.Lock()
	defer mu.Unlock()
	record(frames, leaf)
}

// RecordThread deduplicates samples across VMs executing on one goroutine.
func RecordThread(thread int64, sampleTick uint64, frames []string, leaf string) {
	mu.Lock()
	defer mu.Unlock()
	if !Active.Load() || sampleTick != Tick() {
		return
	}
	if threadTick != sampleTick {
		clear(threads)
		threadTick = sampleTick
	}
	if _, exists := threads[thread]; exists {
		return
	}
	threads[thread] = struct{}{}
	record(frames, leaf)
}

func record(frames []string, leaf string) {
	key := strings.Join(frames, ";")
	stacks[key]++
	if leaf != "" {
		lines[leaf]++
	}
	samples++
}

// Finish stops profiling and writes the report (once; later calls do nothing).
func Finish() {
	if !Active.Load() {
		return
	}
	done.Do(func() {
		close(stop)
		mu.Lock()
		defer mu.Unlock()
		if err := write(); err != nil {
			fmt.Fprintf(os.Stderr, "geblang: profile: %v\n", err)
		}
	})
}

// Exit finishes profiling and exits the process with code.
func Exit(code int) {
	Finish()
	os.Exit(code)
}

// FoldedPath is where the folded stacks are written beside a report.
func FoldedPath(reportPath string) string {
	if filepath.Ext(reportPath) == ".folded" {
		return reportPath + ".folded"
	}
	return strings.TrimSuffix(reportPath, filepath.Ext(reportPath)) + ".folded"
}

func write() error {
	report, err := os.Create(path)
	if err != nil {
		return err
	}
	defer report.Close()
	writeReport(report, time.Since(started))
	folded, err := os.Create(FoldedPath(path))
	if err != nil {
		return err
	}
	defer folded.Close()
	keys := make([]string, 0, len(stacks))
	for k := range stacks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(folded, "%s %d\n", k, stacks[k])
	}
	return nil
}

type entry struct {
	name  string
	count int
}

func ranked(counts map[string]int) []entry {
	out := make([]entry, 0, len(counts))
	for name, n := range counts {
		out = append(out, entry{name, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].name < out[j].name
	})
	return out
}

// topRows is how many rows each table of the report lists.
const topRows = 40

func writeReport(w io.Writer, elapsed time.Duration) {
	own := map[string]int{}
	total := map[string]int{}
	for key, n := range stacks {
		frames := strings.Split(key, ";")
		own[frames[len(frames)-1]] += n
		seen := map[string]bool{}
		for _, f := range frames {
			if !seen[f] {
				seen[f] = true
				total[f] += n
			}
		}
	}
	fmt.Fprintf(w, "Geblang profile: %d samples (about %v of running code each) over %v.\n",
		samples, Interval, elapsed.Round(time.Millisecond))
	fmt.Fprintf(w, "Folded stacks for flame graphs: %s\n", FoldedPath(path))
	table := func(title string, counts map[string]int) {
		fmt.Fprintf(w, "\n%s\n%8s %8s  %s\n", title, "samples", "share", "")
		for i, e := range ranked(counts) {
			if i == topRows {
				break
			}
			fmt.Fprintf(w, "%8d %7.2f%%  %s\n", e.count, 100*float64(e.count)/float64(max(samples, 1)), e.name)
		}
	}
	table("Own time (the function running when sampled):", own)
	table("Total time (the function or anything it called):", total)
	table("Hottest lines (own time):", lines)
}
