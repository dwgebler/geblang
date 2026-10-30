package native

import (
	"fmt"
	"geblang/internal/runtime"
	"math/big"
	"sync"
	"testing"
	"time"
)

func TestBoundedCacheReclaimsExpiredMostRecentBeforeEviction(t *testing.T) {
	now := int64(10)
	cache := &boundedCacheStore{capacity: big.NewInt(2), entries: map[string]*cachePayloadEntry{}, now: func() time.Time { return time.Unix(now, 0) }}
	cache.set("expired", runtime.String{Value: "old"}, big.NewInt(11))
	cache.set("live", runtime.String{Value: "keep"}, big.NewInt(20))
	cache.get("expired")

	now = 12
	cache.set("new", runtime.String{Value: "new"}, big.NewInt(30))

	if value := cache.get("live"); value.Inspect() != "keep" {
		t.Fatalf("live entry evicted: %v", value)
	}
	if !runtime.IsNull(cache.get("expired")) {
		t.Fatal("expired value returned")
	}
	if cache.get("new").Inspect() != "new" {
		t.Fatal("new value missing")
	}
}
func TestBoundedCacheSnapshotsShareStateAndConcurrentAccess(t *testing.T) {
	registry := NewBuiltinRegistry()
	handle, err := registry.Call("store", "_cacheNew", []runtime.Value{runtime.NewInt64(32)})
	if err != nil {
		t.Fatal(err)
	}
	copy := runtime.CloneSnapshot(handle)
	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := 0; i < 100; i++ {
				key := runtime.String{Value: fmt.Sprintf("key-%d", i%16)}
				_, err := registry.Call("store", "_cacheSet", []runtime.Value{copy, key, runtime.String{Value: "data"}, runtime.NewInt64(time.Now().Unix() + 3600)})
				if err != nil {
					t.Error(err)
					return
				}
				if _, err = registry.Call("store", "_cacheGet", []runtime.Value{handle, key}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	group.Wait()
	value, err := registry.Call("store", "_cacheGet", []runtime.Value{handle, runtime.String{Value: "key-0"}})
	if err != nil || value.Inspect() != "data" {
		t.Fatalf("snapshot lost state: %v, %v", value, err)
	}
}
func TestBoundedCacheOverwritesRefreshExpiryWithoutLosingCapacity(t *testing.T) {
	now := int64(10)
	cache := &boundedCacheStore{capacity: big.NewInt(2), entries: map[string]*cachePayloadEntry{}, now: func() time.Time { return time.Unix(now, 0) }}
	cache.set("a", runtime.String{Value: "first"}, big.NewInt(11))
	cache.set("b", runtime.String{Value: "second"}, big.NewInt(30))
	for i := 0; i < 100; i++ {
		cache.set("a", runtime.String{Value: "updated"}, big.NewInt(40))
	}
	now = 12
	cache.set("c", runtime.String{Value: "third"}, big.NewInt(50))
	if cache.get("a").Inspect() != "updated" || !runtime.IsNull(cache.get("b")) {
		t.Fatal("overwrite did not preserve recency and expiry")
	}
	cache.delete("a")
	now = 100
	if !runtime.IsNull(cache.get("c")) {
		t.Fatal("deleted/expired entry survived")
	}
}
