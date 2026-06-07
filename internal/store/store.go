// Package store provides a thread-safe in-memory map of deployment records.
// Data is not persisted; restarting the API clears history (cluster objects remain).
package store

import (
	"sync"

	"kube-deploy/internal/model"
)

// Store holds deployment records keyed by UUID.
type Store struct {
	mu          sync.RWMutex
	deployments map[string]*model.DeploymentRecord
}

// New returns an empty store.
func New() *Store {
	return &Store{deployments: make(map[string]*model.DeploymentRecord)}
}

// Save inserts or replaces a record (defensive copy to avoid races with callers).
func (s *Store) Save(rec *model.DeploymentRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *rec
	s.deployments[rec.ID] = &copy
}

// Get returns a copy of the record and whether the id exists.
func (s *Store) Get(id string) (*model.DeploymentRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.deployments[id]
	if !ok {
		return nil, false
	}
	copy := *rec
	return &copy, true
}

// List returns all deployments (order is undefined).
func (s *Store) List() []model.DeploymentRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.DeploymentRecord, 0, len(s.deployments))
	for _, rec := range s.deployments {
		out = append(out, *rec)
	}
	return out
}

// Update runs fn on the stored record when id exists; fn mutates the in-store copy.
func (s *Store) Update(id string, fn func(*model.DeploymentRecord)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.deployments[id]
	if !ok {
		return false
	}
	fn(rec)
	copy := *rec
	s.deployments[id] = &copy
	return true
}
