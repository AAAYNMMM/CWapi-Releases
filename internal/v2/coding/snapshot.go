package coding

import "sort"

type RuntimeSnapshot struct {
	State        string   `json:"state"`
	Active       int      `json:"active"`
	Repositories []string `json:"repositories,omitempty"`
}

func (s *Service) RuntimeSnapshot() RuntimeSnapshot {
	if s == nil {
		return RuntimeSnapshot{State: "unavailable"}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	repositories := make([]string, 0, len(s.active))
	for repository, owner := range s.active {
		if owner != "" {
			repositories = append(repositories, repository)
		}
	}
	sort.Strings(repositories)
	state := "ready"
	if s.closed {
		state = "closed"
	} else if len(repositories) > 0 {
		state = "active"
	}
	return RuntimeSnapshot{State: state, Active: len(repositories), Repositories: repositories}
}
