package certmanager

import (
	"container/list"
	"crypto/tls"
	"sync"
	"time"
)

type cacheEntry struct {
	cert        *tls.Certificate
	expiresAt   int64
	listElement *list.Element
}

// CertLRUCache is an LRU cache of TLS certificates.
type CertLRUCache struct {
	sync.Mutex

	// maxSize is the maximum number of certificates the cache can store.
	maxSize int
	// list is the doubly linked list used for LRU eviction.
	list *list.List
	// cache is the map of host to certificate.
	cache map[string]cacheEntry
}

// NewCertLRUCache initializes a certificate LRU cache with given parameters.
func NewCertLRUCache(maxSize int, cleanupInterval time.Duration) *CertLRUCache {
	c := CertLRUCache{
		cache:   make(map[string]cacheEntry),
		list:    list.New(),
		maxSize: maxSize,
	}

	go func() {
		// Periodically remove expired entries.
		// This function never exits, which is fine since the CertManager gets accessed via a singleton. Though, be careful with spawning a lot of CertManagers or caches in tests.
		ticker := time.NewTicker(cleanupInterval)
		for range ticker.C {
			c.Lock()
			for e, entry := range c.cache {
				if time.Now().Unix() > entry.expiresAt {
					c.list.Remove(entry.listElement)
					delete(c.cache, e)
				}
			}
			c.Unlock()
		}
	}()

	return &c
}

// Get returns the certificate for the given host, or nil if it is not cached.
func (c *CertLRUCache) Get(host string) *tls.Certificate {
	c.Lock()
	defer c.Unlock()

	entry, ok := c.cache[host]
	if !ok {
		return nil
	}
	if time.Now().Unix() > entry.expiresAt {
		c.list.Remove(entry.listElement)
		delete(c.cache, host)
		return nil
	}

	c.list.MoveToFront(entry.listElement)

	return entry.cert
}

// Put adds the certificate for the given host to the cache.
func (c *CertLRUCache) Put(host string, expiresAt time.Time, cert *tls.Certificate) {
	c.Lock()
	defer c.Unlock()

	if e, ok := c.cache[host]; ok {
		c.list.MoveToFront(e.listElement)
		c.cache[host] = cacheEntry{
			cert:        cert,
			expiresAt:   expiresAt.Unix(),
			listElement: e.listElement,
		}
		return
	}

	if c.list.Len() >= c.maxSize {
		// Evict the least recently used host.
		e := c.list.Back()
		c.list.Remove(e)
		delete(c.cache, e.Value.(string))
	}

	listElement := c.list.PushFront(host)
	c.cache[host] = cacheEntry{
		cert:        cert,
		expiresAt:   expiresAt.Unix(),
		listElement: listElement,
	}
}

// Purge clears the cache.
func (c *CertLRUCache) Purge() {
	c.Lock()
	defer c.Unlock()

	c.cache = make(map[string]cacheEntry)
	c.list = list.New()
}
