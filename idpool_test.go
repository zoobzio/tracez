package tracez

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestIDPoolBasicOperation tests basic ID pool functionality.
func TestIDPoolBasicOperation(t *testing.T) {
	factory := func() string { return "test-id" }
	pool := NewIDPool(10, factory)
	defer pool.Close()

	// Should get ID from pool.
	id := pool.Get()
	if id != "test-id" {
		t.Errorf("Expected 'test-id', got %s", id)
	}
}

// TestIDPoolEmpty tests behavior when pool is empty.
func TestIDPoolEmpty(t *testing.T) {
	var callCount int
	var mu sync.Mutex
	factory := func() string {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		return "direct-id"
	}

	// Very small pool that will be empty.
	pool := NewIDPool(1, factory)
	defer pool.Close()

	// First few calls should drain pool and use factory.
	ids := make([]string, 5)
	for i := range ids {
		ids[i] = pool.Get()
	}

	// Should have called factory multiple times (pool + direct).
	mu.Lock()
	finalCount := callCount
	mu.Unlock()
	if finalCount < 2 {
		t.Errorf("Expected factory to be called multiple times, got %d", finalCount)
	}

	for _, id := range ids {
		if id != "direct-id" {
			t.Errorf("Expected 'direct-id', got %s", id)
		}
	}
}

// TestIDPoolConcurrentAccess tests concurrent access to ID pool.
func TestIDPoolConcurrentAccess(t *testing.T) {
	counter := 0
	mu := sync.Mutex{}
	factory := func() string {
		mu.Lock()
		defer mu.Unlock()
		counter++
		return "concurrent-id"
	}

	pool := NewIDPool(50, factory)
	defer pool.Close()

	var wg sync.WaitGroup
	numGoroutines := 10
	idsPerGoroutine := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < idsPerGoroutine; j++ {
				id := pool.Get()
				if id != "concurrent-id" {
					t.Errorf("Expected 'concurrent-id', got %s", id)
				}
			}
		}()
	}

	wg.Wait()

	// Should have generated some IDs.
	mu.Lock()
	finalCounter := counter
	mu.Unlock()

	if finalCounter == 0 {
		t.Error("Factory was never called")
	}
}

// TestIDPoolCleanShutdown tests that pools shut down cleanly.
func TestIDPoolCleanShutdown(t *testing.T) {
	factory := func() string { return "shutdown-test" }
	pool := NewIDPool(10, factory)

	// Get goroutine count before.
	before := runtime.NumGoroutine()

	// Close pool.
	pool.Close()

	// Give time for cleanup.
	time.Sleep(10 * time.Millisecond)

	// Should not have leaked goroutines.
	after := runtime.NumGoroutine()
	if after > before {
		t.Errorf("Goroutine leak detected: %d -> %d", before, after)
	}

	// Multiple closes should be safe.
	pool.Close()
}
