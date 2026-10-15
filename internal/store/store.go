// Package store recuerda qué ofertas ya se han notificado.
// MVP: un fichero JSON. Si crece, se cambia por SQLite sin tocar el resto.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	mu    sync.Mutex
	path  string
	seen  map[string]time.Time
	isNew bool // el fichero no existía: primera ejecución
}

// Open carga el estado; si el fichero no existe empieza vacío.
func Open(path string) (*Store, error) {
	s := &Store{path: path, seen: map[string]time.Time{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		s.isNew = true
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.seen); err != nil {
		return nil, err
	}
	return s, nil
}

// FirstRun indica que no había estado previo.
func (s *Store) FirstRun() bool { return s.isNew }

func (s *Store) Seen(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.seen[key]
	return ok
}

func (s *Store) Mark(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen[key] = time.Now()
}

// Save poda entradas más antiguas que maxAge y escribe de forma atómica.
func (s *Store) Save(maxAge time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for k, t := range s.seen {
		if t.Before(cutoff) {
			delete(s.seen, k)
		}
	}
	data, err := json.MarshalIndent(s.seen, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.isNew = false
	return nil
}
