// internal/cache/cache.go
package cache

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"lsengine/internal/config"
)

type Cache struct {
	mu          sync.RWMutex
	items       map[string]cacheItem
	maxSize     int
	defaultTTL  time.Duration
	cleanupStop chan struct{}
}

type cacheItem struct {
	value      interface{}
	expiration int64
}

func NewCache(maxSize int, defaultTTL time.Duration) *Cache {
	c := &Cache{
		items:       make(map[string]cacheItem),
		maxSize:     maxSize,
		defaultTTL:  defaultTTL,
		cleanupStop: make(chan struct{}),
	}
	go c.cleanup()
	return c
}

func (c *Cache) Set(key string, value interface{}, ttl ...time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.items) >= c.maxSize {
		for k := range c.items {
			delete(c.items, k)
			break
		}
	}

	exp := time.Now().Add(c.defaultTTL).UnixNano()
	if len(ttl) > 0 {
		exp = time.Now().Add(ttl[0]).UnixNano()
	}

	c.items[key] = cacheItem{
		value:      value,
		expiration: exp,
	}
}

func (c *Cache) Get(key string) interface{} {
	c.mu.RLock()
	item, exists := c.items[key]
	c.mu.RUnlock()

	if !exists {
		return nil
	}

	if time.Now().UnixNano() > item.expiration {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()
		return nil
	}

	return item.value
}

func (c *Cache) Del(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]cacheItem)
}

func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

func (c *Cache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now().UnixNano()
			for k, v := range c.items {
				if now > v.expiration {
					delete(c.items, k)
				}
			}
			c.mu.Unlock()
		case <-c.cleanupStop:
			ticker.Stop()
			return
		}
	}
}

func (c *Cache) Close() {
	close(c.cleanupStop)
}

type CacheManager struct {
	Local  *Cache
	Redis  *redis.Client
	mu     sync.RWMutex
	Config *config.CacheConfig
}

func NewCacheManager(cfg *config.CacheConfig) *CacheManager {
	cm := &CacheManager{
		Local:  NewCache(cfg.Size, cfg.TTL),
		Config: cfg,
	}
	if cfg.Distributed && cfg.RedisURL != "" {
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err == nil {
			cm.Redis = redis.NewClient(opt)
		}
	}
	return cm
}

func (cm *CacheManager) Get(key string) interface{} {
	if cm.Config.Distributed && cm.Redis != nil {
		val, err := cm.Redis.Get(context.Background(), key).Result()
		if err == nil {
			var result interface{}
			json.Unmarshal([]byte(val), &result)
			cm.Local.Set(key, result)
			return result
		}
	}
	return cm.Local.Get(key)
}

func (cm *CacheManager) Set(key string, value interface{}, ttl ...time.Duration) {
	if cm.Config.Distributed && cm.Redis != nil {
		data, _ := json.Marshal(value)
		expiration := cm.Config.TTL
		if len(ttl) > 0 {
			expiration = ttl[0]
		}
		cm.Redis.Set(context.Background(), key, data, expiration)
	}
	cm.Local.Set(key, value, ttl...)
}

func (cm *CacheManager) Del(key string) {
	if cm.Config.Distributed && cm.Redis != nil {
		cm.Redis.Del(context.Background(), key)
	}
	cm.Local.Del(key)
}

func (cm *CacheManager) Clear() {
	cm.Local.Clear()
	if cm.Config.Distributed && cm.Redis != nil {
		cm.Redis.FlushAll(context.Background())
	}
}

func (cm *CacheManager) Close() {
	cm.Local.Close()
	if cm.Redis != nil {
		cm.Redis.Close()
	}
}