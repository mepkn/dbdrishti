package db

import (
	_ "embed"
)

//go:embed sample.sql
var sampleSQL string

// LoadSample (re)creates the "shop" schema: a small store with one-to-many,
// many-to-many and self-referencing foreign keys, and rows chosen so every
// join type gives a different answer.
func (s *Service) LoadSample() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireConn(); err != nil {
		return err
	}
	c, cancel := ctx()
	defer cancel()
	_, err := s.exec(c, sampleSQL)
	return err
}
