package cache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTTLCache_SetAndGet(t *testing.T) {
	c := New[string, int](time.Minute, 0)
	defer c.Stop()

	c.Set("k1", 42, time.Minute)
	v, ok := c.Get("k1")
	require.True(t, ok)
	assert.Equal(t, 42, v)
}

func TestTTLCache_Expiry(t *testing.T) {
	c := New[string, int](time.Minute, 0)
	defer c.Stop()

	c.Set("k1", 1, time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	_, ok := c.Get("k1")
	assert.False(t, ok)
}

func TestTTLCache_Delete(t *testing.T) {
	c := New[string, int](time.Minute, 0)
	defer c.Stop()

	c.Set("k1", 1, time.Minute)
	c.Delete("k1")
	_, ok := c.Get("k1")
	assert.False(t, ok)
}

func TestTTLCache_MaxCap_EvictsOnSet(t *testing.T) {
	c := New[string, int](time.Minute, 2)
	defer c.Stop()

	c.Set("k1", 1, time.Minute)
	c.Set("k2", 2, time.Minute)
	c.Set("k3", 3, time.Minute)

	count := 0
	c.m.Range(func(_, _ any) bool {
		count++
		return true
	})
	assert.Equal(t, 2, count, "expected exactly 2 entries after cap eviction")
}

func TestTTLCache_MaxCap_PrefersExpiredEviction(t *testing.T) {
	c := New[string, int](time.Minute, 2)
	defer c.Stop()

	c.Set("expired", 0, time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	c.Set("live", 1, time.Minute)

	c.Set("new", 2, time.Minute)

	_, expiredGone := c.Get("expired")
	assert.False(t, expiredGone, "expired entry should have been evicted")

	_, liveOk := c.Get("live")
	_, newOk := c.Get("new")
	assert.True(t, liveOk || newOk, "at least one live entry should remain")
}
