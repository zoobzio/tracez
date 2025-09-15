package tracez

import (
	"sync"
)

// IDPool manages a pool of pre-generated IDs to amortize crypto/rand overhead.
type IDPool struct {
	factory func() string
	ids     chan string
	stopCh  chan struct{}
	mu      sync.Mutex
	closed  bool
}

// NewIDPool creates a new ID pool with the specified capacity.
func NewIDPool(capacity int, factory func() string) *IDPool {
	pool := &IDPool{
		ids:     make(chan string, capacity),
		factory: factory,
		stopCh:  make(chan struct{}),
	}
	// Start background refill goroutine.
	go pool.refill()
	return pool
}

// Get retrieves an ID from the pool or generates one if pool is empty.
func (p *IDPool) Get() string {
	select {
	case id := <-p.ids:
		return id
	default:
		// Pool empty, generate directly (fallback for burst load).
		return p.factory()
	}
}

// refill maintains the pool by generating IDs in background.
func (p *IDPool) refill() {
	for {
		select {
		case <-p.stopCh:
			return
		default:
			// Only generate if pool has capacity.
			select {
			case p.ids <- p.factory():
				// Successfully added ID to pool.
			case <-p.stopCh:
				return
			}
		}
	}
}

// Close shuts down the ID pool gracefully.
func (p *IDPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.closed {
		close(p.stopCh)
		p.closed = true
	}
}
