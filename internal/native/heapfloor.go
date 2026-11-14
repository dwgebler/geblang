package native

import "os"

const heapFloorSize = 32 << 20

// heapFloor is never written: it only raises the collector's heap goal so small heaps are not collected every few megabytes.
var heapFloor []byte

// InstallHeapFloor defers garbage collection until about 32 MB is allocated, unless the environment tunes the collector itself.
func InstallHeapFloor() {
	if heapFloor != nil || os.Getenv("GOGC") != "" || os.Getenv("GOMEMLIMIT") != "" {
		return
	}
	heapFloor = make([]byte, heapFloorSize)
}

// HeapFloorBytes is the untouched allocation memory reports subtract so they show program data only.
func HeapFloorBytes() int64 {
	return int64(len(heapFloor))
}
