package fstcache

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TelenLiu/WeTextProcessing-go/libs/pynini"
)

// Item holds an FST and its last access time.
// Fields are unexported to prevent external mutation.
type Item struct {
	fst      *pynini.Fst
	lastUsed time.Time
	mu       sync.Mutex
}

// Fst returns the FST pointer safely.
func (it *Item) Fst() *pynini.Fst {
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.fst
}

// isExpired checks if the item has expired, without locking.
// Caller must hold it.mu.
func (it *Item) isExpired(deadline time.Time) bool {
	return it.fst != nil && it.lastUsed.Before(deadline)
}

// Manager manages FST lifecycle with memory caching, disk persistence,
// and TTL-based eviction.
type Manager struct {
	cacheDir string
	entries  sync.Map
	ttl      time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
	started  atomic.Bool
}

// New creates a new FST cache manager.
// cacheDir: directory for disk-backed FST files.
// ttl: time-to-live for memory entries (default 10 min if zero).
func New(cacheDir string, ttl ...time.Duration) *Manager {
	d := 10 * time.Minute
	if len(ttl) > 0 && ttl[0] > 0 {
		d = ttl[0]
	}
	os.MkdirAll(cacheDir, 0755)
	return &Manager{
		cacheDir: cacheDir,
		ttl:      d,
		stopCh:   make(chan struct{}),
	}
}

// cachePath returns the disk path for a given key.
func (m *Manager) cachePath(key string) string {
	return filepath.Join(m.cacheDir, key+".fst")
}

// GetOrBuild returns an FST from memory cache, disk cache, or builds it.
//   - Memory hit: refresh lastUsed, return immediately (~100ns)
//   - Disk hit: load to memory, return
//   - Cache miss: call builder, write to disk, store in memory, return
//
// Safe for concurrent use.
func (m *Manager) GetOrBuild(key string, builder func() *pynini.Fst) *pynini.Fst {
	// 1. Fast path: memory cache hit
	if v, ok := m.entries.Load(key); ok {
		item := v.(*Item)
		item.mu.Lock()
		if item.fst != nil {
			item.lastUsed = time.Now()
			fst := item.fst
			item.mu.Unlock()
			return fst
		}
		// Was evicted from memory, try disk reload
		if fst, err := pynini.FstRead(m.cachePath(key)); err == nil && len(fst.States) > 0 {
			item.fst = fst
			item.lastUsed = time.Now()
			item.mu.Unlock()
			return fst
		}
		item.mu.Unlock()
	}

	// 2. Disk cache hit (first load or after eviction+gc)
	if fst, err := pynini.FstRead(m.cachePath(key)); err == nil && len(fst.States) > 0 {
		item := &Item{fst: fst, lastUsed: time.Now()}
		m.entries.Store(key, item)
		return fst
	}

	// 3. Cache miss: build from scratch
	fst := builder()
	if fst == nil || len(fst.States) == 0 {
		return fst
	}

	// Write to disk
	if err := fst.Write(m.cachePath(key)); err != nil {
		// Write failed, still return the FST but don't cache on disk
		_ = err
	}

	// Store in memory
	item := &Item{fst: fst, lastUsed: time.Now()}
	m.entries.Store(key, item)
	return fst
}

// SetTTL sets the time-to-live for memory entries.
func (m *Manager) SetTTL(d time.Duration) {
	if d > 0 {
		m.ttl = d
	}
}

// StartEviction starts a background goroutine that evicts expired entries.
// At most one eviction goroutine is started; subsequent calls are no-ops.
// Evicted entries are freed from memory; disk cache is preserved.
func (m *Manager) StartEviction() {
	if m.started.Load() {
		return
	}
	m.started.Store(true)
	go m.evictionLoop()
}

func (m *Manager) evictionLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.evict()
		case <-m.stopCh:
			return
		}
	}
}

// StopEviction stops the background eviction goroutine.
// Safe for concurrent use; multiple calls are harmless.
func (m *Manager) StopEviction() {
	if !m.started.Load() {
		return
	}
	m.started.Store(false)
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
}

func (m *Manager) evict() {
	deadline := time.Now().Add(-m.ttl)
	m.entries.Range(func(key, value interface{}) bool {
		item := value.(*Item)
		item.mu.Lock()
		if item.isExpired(deadline) {
			item.fst = nil // free memory; disk cache preserved
		}
		item.mu.Unlock()
		return true
	})
}

// EvictNow immediately removes all expired entries from memory.
func (m *Manager) EvictNow() {
	m.evict()
}

// Size returns the number of entries currently held in memory.
func (m *Manager) Size() int {
	count := 0
	m.entries.Range(func(_, value interface{}) bool {
		item := value.(*Item)
		item.mu.Lock()
		if item.fst != nil {
			count++
		}
		item.mu.Unlock()
		return true
	})
	return count
}

// Clear removes all entries from memory (disk cache is preserved).
func (m *Manager) Clear() {
	m.entries.Range(func(_, value interface{}) bool {
		item := value.(*Item)
		item.mu.Lock()
		item.fst = nil
		item.mu.Unlock()
		return true
	})
}

// Invalidate removes an entry from both memory and disk cache.
func (m *Manager) Invalidate(key string) {
	m.entries.Delete(key)
	os.Remove(m.cachePath(key))
}

// DiskSize returns the total size of cached FST files on disk.
func (m *Manager) DiskSize() int64 {
	var total int64
	entries, err := os.ReadDir(m.cacheDir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".fst" {
			if info, err := e.Info(); err == nil {
				total += info.Size()
			}
		}
	}
	return total
}

// FstItems returns all currently cached keys for inspection.
func (m *Manager) Keys() []string {
	var keys []string
	m.entries.Range(func(key, value interface{}) bool {
		keys = append(keys, key.(string))
		return true
	})
	return keys
}