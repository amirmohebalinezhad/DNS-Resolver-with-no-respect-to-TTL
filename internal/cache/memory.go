package cache

import (
	"sync"
	"time"
)

type MemoryStore struct {
	mu          sync.RWMutex
	entries     map[string]*Entry
	persistence Persistence
}

func NewMemoryStore(persistence Persistence) *MemoryStore {
	return &MemoryStore{
		entries:     make(map[string]*Entry),
		persistence: persistence,
	}
}

func (s *MemoryStore) LoadFromPersistence(now time.Time) (int, error) {
	if s.persistence == nil {
		return 0, nil
	}
	entries, err := s.persistence.Load(now)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range entries {
		if entry == nil || entry.Key == "" {
			continue
		}
		s.entries[entry.Key] = entry.Clone()
	}
	return len(entries), nil
}

func (s *MemoryStore) Get(key string) (*Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[key]
	if !ok {
		return nil, false
	}
	return entry.Clone(), true
}

func (s *MemoryStore) Set(entry *Entry) error {
	if entry == nil || entry.Key == "" {
		return nil
	}
	clone := entry.Clone()
	s.mu.Lock()
	s.entries[clone.Key] = clone
	s.mu.Unlock()

	if s.persistence != nil {
		return s.persistence.Save(clone)
	}
	return nil
}

func (s *MemoryStore) Delete(key string) error {
	s.mu.Lock()
	delete(s.entries, key)
	s.mu.Unlock()

	if s.persistence != nil {
		return s.persistence.Delete(key)
	}
	return nil
}

func (s *MemoryStore) Flush() error {
	s.mu.Lock()
	s.entries = make(map[string]*Entry)
	s.mu.Unlock()

	if s.persistence != nil {
		return s.persistence.Flush()
	}
	return nil
}

func (s *MemoryStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

func (s *MemoryStore) Close() error {
	if s.persistence == nil {
		return nil
	}
	return s.persistence.Close()
}
