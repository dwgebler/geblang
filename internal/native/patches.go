package native

import (
	"sync"
	"sync/atomic"
)

// Test patches (test.mock) are process-wide so every evaluator, VM and module VM sees the same overrides.
var (
	patchMu         sync.RWMutex
	patchTable      = map[string]Function{}
	patchGeneration atomic.Uint64
	patchCount      atomic.Int64
)

// PatchGeneration changes whenever the patch table does; native call caches compare against it.
func PatchGeneration() uint64 { return patchGeneration.Load() }

// LookupPatch returns the active test patch for a "module.name" key, or nil.
func LookupPatch(key string) Function {
	if patchCount.Load() == 0 {
		return nil
	}
	patchMu.RLock()
	defer patchMu.RUnlock()
	return patchTable[key]
}

func setPatches(table map[string]Function) {
	patchTable = table
	patchCount.Store(int64(len(table)))
	patchGeneration.Add(1)
}

func globalPatch(key string, fn Function) {
	patchMu.Lock()
	defer patchMu.Unlock()
	next := make(map[string]Function, len(patchTable)+1)
	for k, v := range patchTable {
		next[k] = v
	}
	next[key] = fn
	setPatches(next)
}

func globalUnpatch(key string) {
	patchMu.Lock()
	defer patchMu.Unlock()
	next := make(map[string]Function, len(patchTable))
	for k, v := range patchTable {
		if k != key {
			next[k] = v
		}
	}
	setPatches(next)
}

func globalSnapshot() map[string]Function {
	patchMu.RLock()
	defer patchMu.RUnlock()
	out := make(map[string]Function, len(patchTable))
	for k, v := range patchTable {
		out[k] = v
	}
	return out
}

func globalRestore(snapshot map[string]Function) {
	patchMu.Lock()
	defer patchMu.Unlock()
	next := make(map[string]Function, len(snapshot))
	for k, v := range snapshot {
		next[k] = v
	}
	setPatches(next)
}
