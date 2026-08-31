package codex

// Alive reports whether the app-server client is still usable for new
// requests. It does not probe the process or start any new work.
func (c *Client) Alive() bool {
	if c == nil || c.closed.Load() || c.cmd == nil {
		return false
	}
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}
