package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/buildinfo"
)

const FrontendReadyMarker = "react-mounted-v1"
const maxGUIProbePayloadBytes = 64 * 1024

func (s *Service) DesktopLifecycleReady(detail string) {
	if s == nil || s.observability == nil {
		return
	}
	if detail == "" {
		detail = "desktop lifecycle ready"
	}
	s.observability.SetComponent("desktop", "healthy", detail)
	s.runtimeInfo("desktop", detail, nil)
}

func (s *Service) DesktopLifecycleError(operation string, err error) {
	if s == nil || s.observability == nil || err == nil {
		return
	}
	if operation == "" {
		operation = "lifecycle"
	}
	s.recordOperationalError("desktop", operation, err)
	s.observability.SetComponent("desktop", "degraded", err.Error())
}

func (s *Service) ReportFrontendReady(marker string) error {
	if s == nil || s.state == nil {
		return errors.New("FRONTEND_READY_STATE_UNAVAILABLE")
	}
	if marker != FrontendReadyMarker {
		return errors.New("FRONTEND_READY_MARKER_INVALID")
	}
	payload, err := json.MarshalIndent(map[string]any{
		"schema": "cwapi.frontend-ready.v1", "marker": marker, "source_commit": buildinfo.Commit(), "reported_at": time.Now().UTC().Format(time.RFC3339Nano),
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := s.writeDesktopEvidence("frontend-ready.json", append(payload, '\n')); err != nil {
		return err
	}
	s.runtimeInfo("desktop", "React frontend reported mounted", map[string]any{"marker": marker})
	return nil
}

// GUIProbeConfig returns an operator-supplied test description to the actual
// packaged React application. It is inert unless the environment variable is
// explicitly present, so production launches do not run probe logic.
func (s *Service) GUIProbeConfig() string {
	value := strings.TrimSpace(os.Getenv("CWAPI_GUI_PROBE_CONFIG"))
	if len(value) > maxGUIProbePayloadBytes {
		return ""
	}
	return value
}

// ReportGUIProbe persists evidence emitted by the real embedded React page.
// The payload is validated as bounded JSON and written only to the fixed state
// evidence path; it cannot choose an arbitrary filesystem destination.
func (s *Service) ReportGUIProbe(raw string) error {
	if s == nil || s.state == nil {
		return errors.New("GUI_PROBE_STATE_UNAVAILABLE")
	}
	if len(raw) == 0 || len(raw) > maxGUIProbePayloadBytes {
		return errors.New("GUI_PROBE_PAYLOAD_INVALID")
	}
	var result map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return errors.New("GUI_PROBE_JSON_INVALID")
	}
	mode, _ := result["mode"].(string)
	success, _ := result["success"].(bool)
	if mode != "first-run" && mode != "workbench" && mode != "real-slack" {
		return errors.New("GUI_PROBE_MODE_INVALID")
	}
	evidence := map[string]any{
		"schema": "cwapi.gui-probe.v1", "mode": mode, "success": success, "source_commit": buildinfo.Commit(),
		"reported_at": time.Now().UTC().Format(time.RFC3339Nano), "result": result,
	}
	payload, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	if err := s.writeDesktopEvidence("gui-probe.json", append(payload, '\n')); err != nil {
		return err
	}
	if success {
		s.runtimeInfo("desktop", "React GUI probe passed", map[string]any{"mode": mode})
	} else {
		s.DesktopLifecycleError("gui.probe", errors.New("React GUI probe failed"))
	}
	return nil
}

func (s *Service) writeDesktopEvidence(name string, payload []byte) error {
	path := filepath.Join(filepath.Dir(s.state.Path()), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, payload, 0o600); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}
