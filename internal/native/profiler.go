package native

import (
	"fmt"
	goruntime "runtime"
	"sync/atomic"
	"time"

	"geblang/internal/runtime"
)

var profilerPeakAlloc int64

func profilerUpdatePeak(current int64) {
	for {
		old := atomic.LoadInt64(&profilerPeakAlloc)
		if current <= old {
			return
		}
		if atomic.CompareAndSwapInt64(&profilerPeakAlloc, old, current) {
			return
		}
	}
}

func registerProfiler(r *Registry) {
	r.Register("profiler", "snapshot", func(args []runtime.Value) (runtime.Value, error) {
		var ms goruntime.MemStats
		goruntime.ReadMemStats(&ms)
		profilerUpdatePeak(int64(ms.HeapAlloc))
		user, sys := profilerCPUNanos()
		return profilerDict(map[string]runtime.Value{
			"wall_ns": runtime.NewInt64(time.Now().UnixNano()),
			// mono_ns backs delta()'s elapsed_ms; wall_ns can jump backwards under load.
			"mono_ns":     runtime.NewInt64(time.Since(monoClockStart).Nanoseconds()),
			"heap_alloc":  runtime.NewInt64(int64(ms.HeapAlloc)),
			"peak_alloc":  runtime.NewInt64(atomic.LoadInt64(&profilerPeakAlloc)),
			"total_alloc": runtime.NewInt64(int64(ms.TotalAlloc)),
			"num_gc":      runtime.NewInt64(int64(ms.NumGC)),
			"cpu_user_ns": runtime.NewInt64(user),
			"cpu_sys_ns":  runtime.NewInt64(sys),
		}), nil
	})

	r.Register("profiler", "delta", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("profiler.delta expects one argument")
		}
		snap, ok := args[0].(runtime.Dict)
		if !ok {
			return nil, fmt.Errorf("profiler.delta expects a snapshot dict")
		}
		var ms goruntime.MemStats
		goruntime.ReadMemStats(&ms)
		nowMonoNS := time.Since(monoClockStart).Nanoseconds()
		user, sys := profilerCPUNanos()

		snapMono := profilerInt64(snap, "mono_ns")
		snapHeap := profilerInt64(snap, "heap_alloc")
		snapTotal := profilerInt64(snap, "total_alloc")
		snapGC := profilerInt64(snap, "num_gc")
		snapUser := profilerInt64(snap, "cpu_user_ns")
		snapSys := profilerInt64(snap, "cpu_sys_ns")

		elapsedNS := nowMonoNS - snapMono
		cpuDelta := (user + sys) - (snapUser + snapSys)
		return profilerDict(map[string]runtime.Value{
			"elapsed_ms": runtime.Float{Value: float64(elapsedNS) / 1e6},
			"cpu_ms":     runtime.Float{Value: float64(cpuDelta) / 1e6},
			"heap_alloc": runtime.NewInt64(int64(ms.HeapAlloc) - snapHeap),
			"allocs":     runtime.NewInt64(int64(ms.TotalAlloc) - snapTotal),
			"gc_count":   runtime.NewInt64(int64(ms.NumGC) - snapGC),
		}), nil
	})

	r.Register("profiler", "memory", func(args []runtime.Value) (runtime.Value, error) {
		var ms goruntime.MemStats
		goruntime.ReadMemStats(&ms)
		profilerUpdatePeak(int64(ms.HeapAlloc))
		return profilerDict(map[string]runtime.Value{
			"heap_alloc":  runtime.NewInt64(int64(ms.HeapAlloc)),
			"peak_alloc":  runtime.NewInt64(atomic.LoadInt64(&profilerPeakAlloc)),
			"heap_sys":    runtime.NewInt64(int64(ms.HeapSys)),
			"stack_sys":   runtime.NewInt64(int64(ms.StackSys)),
			"total_alloc": runtime.NewInt64(int64(ms.TotalAlloc)),
			"gc_count":    runtime.NewInt64(int64(ms.NumGC)),
		}), nil
	})

	r.Register("profiler", "peak", func(args []runtime.Value) (runtime.Value, error) {
		var ms goruntime.MemStats
		goruntime.ReadMemStats(&ms)
		profilerUpdatePeak(int64(ms.HeapAlloc))
		return profilerDict(map[string]runtime.Value{
			"peak_alloc": runtime.NewInt64(atomic.LoadInt64(&profilerPeakAlloc)),
		}), nil
	})

	r.Register("profiler", "cpu", func(args []runtime.Value) (runtime.Value, error) {
		user, sys := profilerCPUNanos()
		return profilerDict(map[string]runtime.Value{
			"user_ms": runtime.Float{Value: float64(user) / 1e6},
			"sys_ms":  runtime.Float{Value: float64(sys) / 1e6},
		}), nil
	})
}

func profilerDict(fields map[string]runtime.Value) runtime.Dict {
	entries := make(map[string]runtime.DictEntry, len(fields))
	for k, v := range fields {
		key := runtime.String{Value: k}
		entries[DictKey(key)] = runtime.DictEntry{Key: key, Value: v}
	}
	return runtime.Dict{Entries: entries}
}

func profilerInt64(d runtime.Dict, key string) int64 {
	k := runtime.String{Value: key}
	e, ok := d.GetEntry(DictKey(k))
	if !ok {
		return 0
	}
	if n, ok := AsInt64(e.Value); ok {
		return n
	}
	return 0
}
