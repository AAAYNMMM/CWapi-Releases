package processruntime

func (r *Registry) systemToken(processID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.entries[processID]
	if item == nil {
		return "", ErrNotFound
	}
	return item.completion.SystemToken, nil
}
