package cache

import (
	"sync"
	"time"
)

type entry[V any] struct {
	value     V
	expiresAt time.Time
}

type TTLCache[K comparable, V any] struct {
	m          sync.Map
	stop       chan struct{}
	maxEntries int
}

func New[K comparable, V any](evictInterval time.Duration, maxEntries int) *TTLCache[K, V] {
	c := &TTLCache[K, V]{stop: make(chan struct{}), maxEntries: maxEntries}
	go c.evict(evictInterval)
	return c
}

func (c *TTLCache[K, V]) Stop() {
	close(c.stop)
}

func (c *TTLCache[K, V]) evict(interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			c.m.Range(func(k, v any) bool {
				if e, ok := v.(entry[V]); ok && time.Now().After(e.expiresAt) {
					c.m.Delete(k)
				}
				return true
			})
		case <-c.stop:
			return
		}
	}
}

func (c *TTLCache[K, V]) evictOne() {
	n := 0
	var firstKey any
	now := time.Now()
	c.m.Range(func(k, v any) bool {
		if n == 0 {
			firstKey = k
		}
		n++
		if e, ok := v.(entry[V]); ok && now.After(e.expiresAt) {
			c.m.Delete(k)
			firstKey = nil
			return false
		}
		return n < 32
	})
	if firstKey != nil {
		c.m.Delete(firstKey)
	}
}

func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	v, ok := c.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	e := v.(entry[V])
	if time.Now().After(e.expiresAt) {
		c.m.Delete(key)
		var zero V
		return zero, false
	}
	return e.value, true
}

func (c *TTLCache[K, V]) Set(key K, value V, ttl time.Duration) {
	if c.maxEntries > 0 {
		size := 0
		c.m.Range(func(_, _ any) bool {
			size++
			return size < c.maxEntries
		})
		if size >= c.maxEntries {
			c.evictOne()
		}
	}
	c.m.Store(key, entry[V]{value: value, expiresAt: time.Now().Add(ttl)})
}

func (c *TTLCache[K, V]) Delete(key K) {
	c.m.Delete(key)
}
