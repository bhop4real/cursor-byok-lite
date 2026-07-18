package bridge

import (
	"errors"
	"os"
	"strings"
	"time"

	"cursor/internal/appdata"
	"cursor/internal/profiler"
)

// ProfilerStatus is the secret-free profiler lifecycle state exposed to Wails.
type ProfilerStatus = profiler.Status

// ProfilerService exposes offline profiler lifecycle controls.
type ProfilerService struct {
	manager *profiler.Manager
}

// NewProfilerService creates the Wails profiler bridge.
func NewProfilerService(manager *profiler.Manager) *ProfilerService {
	return &ProfilerService{manager: manager}
}

// StartProfiling starts one bounded offline profile session.
func (service *ProfilerService) StartProfiling(traceLimitSeconds int) (ProfilerStatus, error) {
	if service == nil || service.manager == nil {
		return ProfilerStatus{}, errors.New("profiler service is not initialized")
	}
	return service.manager.Start(time.Duration(traceLimitSeconds) * time.Second)
}

// StopProfiling stops the current session and writes snapshot profiles.
func (service *ProfilerService) StopProfiling() (ProfilerStatus, error) {
	if service == nil || service.manager == nil {
		return ProfilerStatus{}, errors.New("profiler service is not initialized")
	}
	return service.manager.Stop()
}

// GetProfilerStatus returns the active or most recently completed session.
func (service *ProfilerService) GetProfilerStatus() ProfilerStatus {
	if service == nil || service.manager == nil {
		return ProfilerStatus{State: "idle"}
	}
	return service.manager.Status()
}

// OpenProfilerDirectory opens the active session, last session, or profiles root.
func (service *ProfilerService) OpenProfilerDirectory() error {
	path := appdata.ProfilesRootPath()
	if service != nil && service.manager != nil {
		if sessionDirectory := strings.TrimSpace(service.manager.Status().Directory); sessionDirectory != "" {
			path = sessionDirectory
		}
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	openDirectory(path)
	return nil
}

// Shutdown stops an active session so the artifacts remain readable.
func (service *ProfilerService) Shutdown() error {
	if service == nil || service.manager == nil {
		return nil
	}
	return service.manager.Close()
}
