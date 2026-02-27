package clockify

import (
	"sync"
	"time"
)

type ProjectCache struct {
	mu       sync.RWMutex
	projects []Project
	fetchedAt time.Time
	ttl      time.Duration
}

func NewProjectCache(ttl time.Duration) *ProjectCache {
	return &ProjectCache{ttl: ttl}
}

func (c *ProjectCache) Get() []Project {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.projects == nil || time.Since(c.fetchedAt) > c.ttl {
		return nil
	}

	result := make([]Project, len(c.projects))
	copy(result, c.projects)
	return result
}

func (c *ProjectCache) Set(projects []Project) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.projects = make([]Project, len(projects))
	copy(c.projects, projects)
	c.fetchedAt = time.Now()
}

func (c *ProjectCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.projects = nil
}
