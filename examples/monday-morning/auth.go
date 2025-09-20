package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zoobzio/clockz"
	"github.com/zoobzio/tracez"
)

// User represents a user with enriched data.
type User struct {
	ID          string
	Name        string
	Permissions []string
	Groups      []string
	Preferences map[string]string
	History     []string
}

// AuthService handles user authentication with caching.
type AuthService struct {
	cache        *Cache
	db           *MockDatabase
	permService  *PermissionService
	groupService *GroupService
	prefService  *PreferenceService
	histService  *HistoryService
	clock        clockz.Clock
	tracer       *tracez.Tracer
	connPool     *ConnectionPool
}

// NewAuthService creates an auth service for production.
func NewAuthService() *AuthService {
	return NewAuthServiceWithClock(clockz.RealClock)
}

// NewAuthServiceWithClock creates an auth service with custom clock.
func NewAuthServiceWithClock(clock clockz.Clock) *AuthService {
	connPool := NewConnectionPool(50) // Limited connection pool
	return &AuthService{
		cache:        NewCache(100),
		db:           NewMockDatabase(clock),
		permService:  NewPermissionService(clock, connPool),
		groupService: NewGroupService(clock, connPool),
		prefService:  NewPreferenceService(clock, connPool),
		histService:  NewHistoryService(clock, connPool),
		clock:        clock,
		connPool:     connPool,
	}
}

// EnableTracing adds distributed tracing to the auth service.
func (a *AuthService) EnableTracing(tracer *tracez.Tracer) {
	a.tracer = tracer
	// Also enable tracing on sub-services
	a.permService.EnableTracing(tracer)
	a.groupService.EnableTracing(tracer)
	a.prefService.EnableTracing(tracer)
	a.histService.EnableTracing(tracer)
}

// Authenticate validates and enriches user data.
// Without tracing: looks like a simple auth check.
// With tracing: reveals the hidden cascade.
func (a *AuthService) Authenticate(ctx context.Context, userID string) (*User, error) {
	// Start tracing if enabled
	var span *tracez.ActiveSpan
	if a.tracer != nil {
		ctx, span = a.tracer.StartSpan(ctx, "authenticate")
		defer span.Finish()
		span.SetTag("user.id", userID)
	}

	// Check cache first
	if a.tracer != nil {
		cacheCtx, cacheSpan := a.tracer.StartSpan(ctx, "cache.get")
		defer cacheSpan.Finish()
		ctx = cacheCtx
	}
	
	if user, ok := a.cache.Get(userID); ok {
		if span != nil {
			span.SetTag("cache.hit", "true")
		}
		return user, nil
	}
	
	if span != nil {
		span.SetTag("cache.hit", "false")
	}

	// Cache miss - fetch from database
	var user *User
	var err error
	
	if a.tracer != nil {
		dbCtx, dbSpan := a.tracer.StartSpan(ctx, "db.get_user")
		user, err = a.db.GetUser(userID)
		dbSpan.Finish()
		ctx = dbCtx
	} else {
		user, err = a.db.GetUser(userID)
	}
	
	if err != nil {
		return nil, err
	}

	// THE HIDDEN CASCADE - each of these makes multiple backend calls
	
	// Fetch permissions (this is the worst offender)
	if a.tracer != nil {
		permCtx, permSpan := a.tracer.StartSpan(ctx, "fetch.permissions")
		user.Permissions = a.permService.FetchPermissions(permCtx, userID)
		permSpan.SetTag("permission.count", fmt.Sprintf("%d", len(user.Permissions)))
		permSpan.Finish()
	} else {
		user.Permissions = a.permService.FetchPermissions(ctx, userID)
	}

	// Fetch groups (recursive parent lookups)
	if a.tracer != nil {
		groupCtx, groupSpan := a.tracer.StartSpan(ctx, "fetch.groups")
		user.Groups = a.groupService.FetchGroups(groupCtx, userID)
		groupSpan.SetTag("group.count", fmt.Sprintf("%d", len(user.Groups)))
		groupSpan.Finish()
	} else {
		user.Groups = a.groupService.FetchGroups(ctx, userID)
	}

	// Fetch preferences (external API calls)
	if a.tracer != nil {
		prefCtx, prefSpan := a.tracer.StartSpan(ctx, "fetch.preferences")
		user.Preferences = a.prefService.FetchPreferences(prefCtx, userID)
		prefSpan.SetTag("preference.count", fmt.Sprintf("%d", len(user.Preferences)))
		prefSpan.Finish()
	} else {
		user.Preferences = a.prefService.FetchPreferences(ctx, userID)
	}

	// Fetch history (log scanning)
	if a.tracer != nil {
		histCtx, histSpan := a.tracer.StartSpan(ctx, "fetch.history")
		user.History = a.histService.FetchRecentHistory(histCtx, userID)
		histSpan.SetTag("history.count", fmt.Sprintf("%d", len(user.History)))
		histSpan.Finish()
	} else {
		user.History = a.histService.FetchRecentHistory(ctx, userID)
	}
	
	a.cache.Set(userID, user)
	return user, nil
}

// Cache is a simple LRU cache.
type Cache struct {
	data map[string]*User
	mu   sync.RWMutex
}

func NewCache(size int) *Cache {
	return &Cache{
		data: make(map[string]*User),
	}
}

func (c *Cache) Get(userID string) (*User, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	user, ok := c.data[userID]
	return user, ok
}

func (c *Cache) Set(userID string, user *User) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[userID] = user
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]*User)
}

// ConnectionPool simulates a limited connection pool that becomes a bottleneck under load.
type ConnectionPool struct {
	semaphore chan struct{}
	activeConns int64
	mu sync.RWMutex
}

func NewConnectionPool(maxConns int) *ConnectionPool {
	return &ConnectionPool{
		semaphore: make(chan struct{}, maxConns),
	}
}

// Acquire blocks until a connection is available.
// Under high concurrency, this becomes the bottleneck.
func (cp *ConnectionPool) Acquire(ctx context.Context) error {
	select {
	case cp.semaphore <- struct{}{}:
		atomic.AddInt64(&cp.activeConns, 1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release returns a connection to the pool.
func (cp *ConnectionPool) Release() {
	select {
	case <-cp.semaphore:
		atomic.AddInt64(&cp.activeConns, -1)
	default:
		// Pool corruption - this shouldn't happen
	}
}

// ActiveConnections returns current active connection count.
func (cp *ConnectionPool) ActiveConnections() int64 {
	return atomic.LoadInt64(&cp.activeConns)
}

// variableLatency adds realistic jitter to base latency.
// Production services don't have fixed response times.
func variableLatency(base time.Duration) time.Duration {
	// Add ±30% jitter
	jitter := float64(base) * 0.3 * (rand.Float64()*2 - 1)
	return base + time.Duration(jitter)
}

// MockDatabase simulates a fast database.
type MockDatabase struct {
	clock clockz.Clock
}

func NewMockDatabase(clock clockz.Clock) *MockDatabase {
	return &MockDatabase{clock: clock}
}

func (db *MockDatabase) GetUser(userID string) (*User, error) {
	// Database is actually fast - not the problem!
	// Skip actual delay - just return instantly
	return &User{
		ID:   userID,
		Name: fmt.Sprintf("User_%s", userID),
	}, nil
}

// PermissionService fetches user permissions.
// THE REAL CULPRIT - makes multiple service calls.
type PermissionService struct {
	clock    clockz.Clock
	tracer   *tracez.Tracer
	connPool *ConnectionPool
}

func NewPermissionService(clock clockz.Clock, connPool *ConnectionPool) *PermissionService {
	return &PermissionService{clock: clock, connPool: connPool}
}

func (p *PermissionService) EnableTracing(tracer *tracez.Tracer) {
	p.tracer = tracer
}

func (p *PermissionService) FetchPermissions(ctx context.Context, userID string) []string {
	permissions := []string{}

	// Call permission service (slow + connection pool contention)
	if p.tracer != nil {
		_, serviceSpan := p.tracer.StartSpan(ctx, "permission.service.call")
		
		// Acquire connection from limited pool
		if err := p.connPool.Acquire(ctx); err != nil {
			serviceSpan.SetTag("error", "connection_timeout")
			serviceSpan.Finish()
			return []string{} // Fail open with minimal permissions
		}
		defer p.connPool.Release()
		
		// Add connection contention delay based on active connections
		activeConns := p.connPool.ActiveConnections()
		contentionDelay := time.Duration(activeConns) * 10 * time.Millisecond
		serviceSpan.SetTag("active_connections", fmt.Sprintf("%d", activeConns))
		serviceSpan.SetTag("contention_delay_ms", fmt.Sprintf("%d", contentionDelay.Milliseconds()))
		
		// Variable latency + contention
		baseDuration := 900 * time.Millisecond
		totalDelay := variableLatency(baseDuration) + contentionDelay
		serviceSpan.SetTag("simulated_delay_ms", fmt.Sprintf("%d", totalDelay.Milliseconds()))
		// Skip actual delay - trace shows the problem
		serviceSpan.Finish()
	} else {
		// Without tracing, still experience the bottleneck
		if err := p.connPool.Acquire(ctx); err != nil {
			return []string{}
		}
		defer p.connPool.Release()
		// Skip actual delay in non-traced path
	}
	permissions = append(permissions, "read", "write")

	// Resolve roles (slow)
	if p.tracer != nil {
		_, roleSpan := p.tracer.StartSpan(ctx, "role.resolver.expand")
		
		if err := p.connPool.Acquire(ctx); err != nil {
			roleSpan.SetTag("error", "connection_timeout")
			roleSpan.Finish()
			return permissions
		}
		defer p.connPool.Release()
		
		activeConns := p.connPool.ActiveConnections()
		contentionDelay := time.Duration(activeConns) * 8 * time.Millisecond
		roleSpan.SetTag("active_connections", fmt.Sprintf("%d", activeConns))
		
		baseDuration := 800 * time.Millisecond
		totalDelay := variableLatency(baseDuration) + contentionDelay
		roleSpan.SetTag("simulated_delay_ms", fmt.Sprintf("%d", totalDelay.Milliseconds()))
		// Skip actual delay - trace shows the problem
		roleSpan.Finish()
	} else {
		if err := p.connPool.Acquire(ctx); err != nil {
			return permissions
		}
		defer p.connPool.Release()
		// Skip actual delay in non-traced path
	}
	permissions = append(permissions, "admin.read", "admin.write")

	// Evaluate policies (slow)
	if p.tracer != nil {
		_, policySpan := p.tracer.StartSpan(ctx, "policy.engine.evaluate")
		
		if err := p.connPool.Acquire(ctx); err != nil {
			policySpan.SetTag("error", "connection_timeout")
			policySpan.Finish()
			return permissions
		}
		defer p.connPool.Release()
		
		activeConns := p.connPool.ActiveConnections()
		contentionDelay := time.Duration(activeConns) * 12 * time.Millisecond
		policySpan.SetTag("active_connections", fmt.Sprintf("%d", activeConns))
		
		baseDuration := 1100 * time.Millisecond
		totalDelay := variableLatency(baseDuration) + contentionDelay
		policySpan.SetTag("simulated_delay_ms", fmt.Sprintf("%d", totalDelay.Milliseconds()))
		// Skip actual delay - trace shows the problem
		policySpan.Finish()
	} else {
		if err := p.connPool.Acquire(ctx); err != nil {
			return permissions
		}
		defer p.connPool.Release()
		// Skip actual delay in non-traced path
	}
	permissions = append(permissions, "billing.view", "reports.generate")

	return permissions
}

// GroupService fetches user groups with recursive lookups.
type GroupService struct {
	clock    clockz.Clock
	tracer   *tracez.Tracer
	connPool *ConnectionPool
}

func NewGroupService(clock clockz.Clock, connPool *ConnectionPool) *GroupService {
	return &GroupService{clock: clock, connPool: connPool}
}

func (g *GroupService) EnableTracing(tracer *tracez.Tracer) {
	g.tracer = tracer
}

func (g *GroupService) FetchGroups(ctx context.Context, userID string) []string {
	groups := []string{}

	// Direct groups
	if g.tracer != nil {
		_, directSpan := g.tracer.StartSpan(ctx, "groups.fetch_direct")
		
		if err := g.connPool.Acquire(ctx); err != nil {
			directSpan.SetTag("error", "connection_timeout")
			directSpan.Finish()
			return []string{}
		}
		defer g.connPool.Release()
		
		activeConns := g.connPool.ActiveConnections()
		contentionDelay := time.Duration(activeConns) * 5 * time.Millisecond
		directSpan.SetTag("active_connections", fmt.Sprintf("%d", activeConns))
		
		totalDelay := variableLatency(500*time.Millisecond) + contentionDelay
		directSpan.SetTag("simulated_delay_ms", fmt.Sprintf("%d", totalDelay.Milliseconds()))
		// Skip actual delay - trace shows the problem
		directSpan.Finish()
	} else {
		if err := g.connPool.Acquire(ctx); err != nil {
			return []string{}
		}
		defer g.connPool.Release()
		// Skip actual delay in non-traced path
	}
	groups = append(groups, "engineering", "backend")

	// Parent groups (recursive)
	if g.tracer != nil {
		_, parentSpan := g.tracer.StartSpan(ctx, "groups.fetch_parents")
		
		if err := g.connPool.Acquire(ctx); err != nil {
			parentSpan.SetTag("error", "connection_timeout")
			parentSpan.Finish()
			return groups
		}
		defer g.connPool.Release()
		
		activeConns := g.connPool.ActiveConnections()
		contentionDelay := time.Duration(activeConns) * 6 * time.Millisecond
		parentSpan.SetTag("active_connections", fmt.Sprintf("%d", activeConns))
		
		totalDelay := variableLatency(600*time.Millisecond) + contentionDelay
		parentSpan.SetTag("simulated_delay_ms", fmt.Sprintf("%d", totalDelay.Milliseconds()))
		// Skip actual delay - trace shows the problem
		parentSpan.Finish()
	} else {
		if err := g.connPool.Acquire(ctx); err != nil {
			return groups
		}
		defer g.connPool.Release()
		// Skip actual delay in non-traced path
	}
	groups = append(groups, "all-staff", "technical")

	// Inherited groups
	if g.tracer != nil {
		_, inheritSpan := g.tracer.StartSpan(ctx, "groups.fetch_inherited")
		
		if err := g.connPool.Acquire(ctx); err != nil {
			inheritSpan.SetTag("error", "connection_timeout")
			inheritSpan.Finish()
			return groups
		}
		defer g.connPool.Release()
		
		activeConns := g.connPool.ActiveConnections()
		contentionDelay := time.Duration(activeConns) * 4 * time.Millisecond
		inheritSpan.SetTag("active_connections", fmt.Sprintf("%d", activeConns))
		
		totalDelay := variableLatency(400*time.Millisecond) + contentionDelay
		inheritSpan.SetTag("simulated_delay_ms", fmt.Sprintf("%d", totalDelay.Milliseconds()))
		// Skip actual delay - trace shows the problem
		inheritSpan.Finish()
	} else {
		if err := g.connPool.Acquire(ctx); err != nil {
			return groups
		}
		defer g.connPool.Release()
		// Skip actual delay in non-traced path
	}
	groups = append(groups, "product-access", "vpn-users")

	return groups
}

// PreferenceService fetches user preferences from external APIs.
type PreferenceService struct {
	clock    clockz.Clock
	tracer   *tracez.Tracer
	connPool *ConnectionPool
}

func NewPreferenceService(clock clockz.Clock, connPool *ConnectionPool) *PreferenceService {
	return &PreferenceService{clock: clock, connPool: connPool}
}

func (p *PreferenceService) EnableTracing(tracer *tracez.Tracer) {
	p.tracer = tracer
}

func (p *PreferenceService) FetchPreferences(ctx context.Context, userID string) map[string]string {
	prefs := make(map[string]string)

	// External API call
	if p.tracer != nil {
		_, apiSpan := p.tracer.StartSpan(ctx, "preferences.external_api")
		
		if err := p.connPool.Acquire(ctx); err != nil {
			apiSpan.SetTag("error", "connection_timeout")
			apiSpan.Finish()
			return map[string]string{"theme": "light"} // Default fallback
		}
		defer p.connPool.Release()
		
		activeConns := p.connPool.ActiveConnections()
		contentionDelay := time.Duration(activeConns) * 3 * time.Millisecond
		apiSpan.SetTag("active_connections", fmt.Sprintf("%d", activeConns))
		
		totalDelay := variableLatency(400*time.Millisecond) + contentionDelay
		apiSpan.SetTag("simulated_delay_ms", fmt.Sprintf("%d", totalDelay.Milliseconds()))
		// Skip actual delay - trace shows the problem
		apiSpan.Finish()
	} else {
		if err := p.connPool.Acquire(ctx); err != nil {
			return map[string]string{"theme": "light"}
		}
		defer p.connPool.Release()
		// Skip actual delay in non-traced path
	}
	prefs["theme"] = "dark"
	prefs["language"] = "en"

	// Legacy system sync
	if p.tracer != nil {
		_, legacySpan := p.tracer.StartSpan(ctx, "preferences.legacy_sync")
		
		if err := p.connPool.Acquire(ctx); err != nil {
			legacySpan.SetTag("error", "connection_timeout")
			legacySpan.Finish()
			return prefs
		}
		defer p.connPool.Release()
		
		activeConns := p.connPool.ActiveConnections()
		contentionDelay := time.Duration(activeConns) * 2 * time.Millisecond
		legacySpan.SetTag("active_connections", fmt.Sprintf("%d", activeConns))
		
		totalDelay := variableLatency(200*time.Millisecond) + contentionDelay
		legacySpan.SetTag("simulated_delay_ms", fmt.Sprintf("%d", totalDelay.Milliseconds()))
		// Skip actual delay - trace shows the problem
		legacySpan.Finish()
	} else {
		if err := p.connPool.Acquire(ctx); err != nil {
			return prefs
		}
		defer p.connPool.Release()
		// Skip actual delay in non-traced path
	}
	prefs["notifications"] = "enabled"

	return prefs
}

// HistoryService fetches user activity history.
type HistoryService struct {
	clock    clockz.Clock
	tracer   *tracez.Tracer
	connPool *ConnectionPool
}

func NewHistoryService(clock clockz.Clock, connPool *ConnectionPool) *HistoryService {
	return &HistoryService{clock: clock, connPool: connPool}
}

func (h *HistoryService) EnableTracing(tracer *tracez.Tracer) {
	h.tracer = tracer
}

func (h *HistoryService) FetchRecentHistory(ctx context.Context, userID string) []string {
	history := []string{}

	// Scan logs (30 days)
	if h.tracer != nil {
		_, scanSpan := h.tracer.StartSpan(ctx, "history.scan_logs")
		
		if err := h.connPool.Acquire(ctx); err != nil {
			scanSpan.SetTag("error", "connection_timeout")
			scanSpan.Finish()
			return []string{} // No history if connection fails
		}
		defer h.connPool.Release()
		
		activeConns := h.connPool.ActiveConnections()
		contentionDelay := time.Duration(activeConns) * 2 * time.Millisecond
		scanSpan.SetTag("active_connections", fmt.Sprintf("%d", activeConns))
		
		totalDelay := variableLatency(300*time.Millisecond) + contentionDelay
		scanSpan.SetTag("simulated_delay_ms", fmt.Sprintf("%d", totalDelay.Milliseconds()))
		// Skip actual delay - trace shows the problem
		scanSpan.Finish()
	} else {
		if err := h.connPool.Acquire(ctx); err != nil {
			return []string{}
		}
		defer h.connPool.Release()
		// Skip actual delay in non-traced path
	}

	history = append(history,
		"2024-03-10: logged in",
		"2024-03-09: updated profile",
		"2024-03-08: changed password",
	)

	return history
}