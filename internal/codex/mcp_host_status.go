package codex

import "strings"

// MCPHostSnapshot exposes only the packaged Toolhost facts needed by the
// desktop/acceptance surface. Reading it never starts the Toolhost.
type MCPHostSnapshot struct {
	Runtime            RuntimeSnapshot `json:"runtime"`
	ExecutableVerified bool            `json:"executable_verified"`
	Running            bool            `json:"running"`
}

func (h *MCPHost) HostSnapshot() MCPHostSnapshot {
	if h == nil || h.service == nil {
		return MCPHostSnapshot{}
	}
	runtime := h.service.Snapshot()
	verified := runtime.Configured && strings.EqualFold(runtime.ExecutableSHA, PinnedExecutableSHA256)

	h.mu.Lock()
	running := h.generation != nil && h.generation.client.Alive()
	h.mu.Unlock()

	return MCPHostSnapshot{
		Runtime:            runtime,
		ExecutableVerified: verified,
		Running:            running,
	}
}
