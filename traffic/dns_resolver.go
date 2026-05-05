package traffic

import (
	"net"
	"sync"
	"time"
)

// DNSCacheEntry stores a resolved hostname with TTL
type DNSCacheEntry struct {
	Hostname  string
	ExpiresAt time.Time
}

// DNSCache provides reverse DNS resolution with TTL-based caching
type DNSCache struct {
	mu    sync.RWMutex
	cache map[string]*DNSCacheEntry
	ttl   time.Duration
}

// NewDNSCache creates a new DNS cache with the specified TTL
func NewDNSCache(ttl time.Duration) *DNSCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &DNSCache{
		cache: make(map[string]*DNSCacheEntry),
		ttl:   ttl,
	}
}

// Resolve performs reverse DNS lookup for an IP address with caching
func (d *DNSCache) Resolve(ip string) (string, error) {
	// Check cache first
	d.mu.RLock()
	if entry, exists := d.cache[ip]; exists {
		if time.Now().Before(entry.ExpiresAt) {
			hostname := entry.Hostname
			d.mu.RUnlock()
			return hostname, nil
		}
	}
	d.mu.RUnlock()

	// Perform reverse DNS lookup
	names, err := net.LookupAddr(ip)
	if err != nil {
		return "", err
	}

	hostname := ip
	if len(names) > 0 {
		hostname = names[0]
	}

	// Store in cache
	d.mu.Lock()
	d.cache[ip] = &DNSCacheEntry{
		Hostname:  hostname,
		ExpiresAt: time.Now().Add(d.ttl),
	}
	d.mu.Unlock()

	return hostname, nil
}

// Cleanup removes expired entries from the cache
func (d *DNSCache) Cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	for ip, entry := range d.cache {
		if now.After(entry.ExpiresAt) {
			delete(d.cache, ip)
		}
	}
}
