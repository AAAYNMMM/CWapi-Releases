package workspace

import (
	"os"
	"path/filepath"
	"sort"
)

type IndexSnapshot struct {
	RepositoryCount int      `json:"repository_count"`
	Repositories    []string `json:"repositories,omitempty"`
	InvalidEntries  int      `json:"invalid_entries,omitempty"`
}

// Index reads durable workspace metadata only. It never fetches or mutates Git.
func (m *Manager) Index() IndexSnapshot {
	if m == nil {
		return IndexSnapshot{}
	}
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return IndexSnapshot{}
	}
	seen := map[string]struct{}{}
	invalid := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		meta, err := loadMetadata(filepath.Join(m.root, entry.Name(), "workspace.json"))
		if err != nil {
			invalid++
			continue
		}
		if meta.Repository == "" {
			invalid++
			continue
		}
		seen[meta.Repository] = struct{}{}
	}
	repositories := make([]string, 0, len(seen))
	for repository := range seen {
		repositories = append(repositories, repository)
	}
	sort.Strings(repositories)
	return IndexSnapshot{RepositoryCount: len(repositories), Repositories: repositories, InvalidEntries: invalid}
}
