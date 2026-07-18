package profiler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	runtimeTrace "runtime/trace"
	"strings"
	"sync"
	"time"
)

const (
	defaultTraceDuration = 5 * time.Minute
	minTraceDuration     = 30 * time.Second
	maxTraceDuration     = 15 * time.Minute
)

var ErrSessionAlreadyRunning = errors.New("a profiler session is already running")
var ErrNoSession = errors.New("no profiler session is running")

// Status is the redacted lifecycle state exposed to the UI.
type Status struct {
	State             string    `json:"state"`
	SessionID         string    `json:"sessionId"`
	Directory         string    `json:"directory"`
	StartedAt         time.Time `json:"startedAt"`
	StoppedAt         time.Time `json:"stoppedAt"`
	TraceLimitSeconds int       `json:"traceLimitSeconds"`
	AutoStopped       bool      `json:"autoStopped"`
	Error             string    `json:"error"`
}

type session struct {
	status        Status
	cpuFile       *os.File
	traceFile     *os.File
	cancelTimer   *time.Timer
	mutexFraction int
}

// Manager owns one offline runtime profiling session at a time.
type Manager struct {
	root   string
	mu     sync.Mutex
	active *session
	last   Status
}

func NewManager(root string) *Manager {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "profiles"
	}
	return &Manager{root: root, last: Status{State: "idle"}}
}

func (manager *Manager) Start(traceDuration time.Duration) (Status, error) {
	if manager == nil {
		return Status{}, errors.New("profiler manager is nil")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.active != nil {
		return manager.active.status, ErrSessionAlreadyRunning
	}

	traceDuration = normalizeTraceDuration(traceDuration)
	sessionID := time.Now().UTC().Format("20060102T150405.000000000Z")
	directory := filepath.Join(manager.root, sessionID)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return Status{}, fmt.Errorf("create profile directory: %w", err)
	}

	cpuFile, err := os.Create(filepath.Join(directory, "cpu.pprof"))
	if err != nil {
		return Status{}, fmt.Errorf("create cpu profile: %w", err)
	}
	traceFile, err := os.Create(filepath.Join(directory, "trace.out"))
	if err != nil {
		_ = cpuFile.Close()
		return Status{}, fmt.Errorf("create runtime trace: %w", err)
	}

	if err := runtimeTrace.Start(traceFile); err != nil {
		_ = traceFile.Close()
		_ = cpuFile.Close()
		return Status{}, fmt.Errorf("start runtime trace: %w", err)
	}
	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		runtimeTrace.Stop()
		_ = traceFile.Close()
		_ = cpuFile.Close()
		return Status{}, fmt.Errorf("start cpu profile: %w", err)
	}

	mutexFraction := runtime.SetMutexProfileFraction(1)
	runtime.SetBlockProfileRate(1)
	active := &session{
		status: Status{
			State:             "running",
			SessionID:         sessionID,
			Directory:         directory,
			StartedAt:         time.Now().UTC(),
			TraceLimitSeconds: int(traceDuration / time.Second),
		},
		cpuFile:       cpuFile,
		traceFile:     traceFile,
		mutexFraction: mutexFraction,
	}
	manager.active = active
	active.cancelTimer = time.AfterFunc(traceDuration, func() {
		_, _ = manager.stop(true)
	})
	return active.status, nil
}

func (manager *Manager) Stop() (Status, error) {
	return manager.stop(false)
}

func (manager *Manager) stop(autoStopped bool) (Status, error) {
	if manager == nil {
		return Status{}, errors.New("profiler manager is nil")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.active == nil {
		if manager.last.State == "stopped" {
			return manager.last, nil
		}
		return manager.last, ErrNoSession
	}
	active := manager.active
	manager.active = nil
	if active.cancelTimer != nil && !autoStopped {
		active.cancelTimer.Stop()
	}

	pprof.StopCPUProfile()
	runtimeTrace.Stop()
	_ = active.cpuFile.Close()
	_ = active.traceFile.Close()
	runtime.SetBlockProfileRate(0)
	runtime.SetMutexProfileFraction(active.mutexFraction)

	status := active.status
	status.State = "stopped"
	status.StoppedAt = time.Now().UTC()
	status.AutoStopped = autoStopped
	var firstErr error
	for _, name := range []string{"goroutine", "heap", "block", "mutex", "threadcreate"} {
		if err := writeProfile(status.Directory, name); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		status.Error = firstErr.Error()
	}
	manager.last = status
	return status, firstErr
}

func writeProfile(directory string, name string) error {
	file, err := os.Create(filepath.Join(directory, name+".pprof"))
	if err != nil {
		return fmt.Errorf("create %s profile: %w", name, err)
	}
	defer file.Close()
	profile := pprof.Lookup(name)
	if profile == nil {
		return fmt.Errorf("profile %s is unavailable", name)
	}
	if err := profile.WriteTo(file, 0); err != nil {
		return fmt.Errorf("write %s profile: %w", name, err)
	}
	return nil
}

func (manager *Manager) Status() Status {
	if manager == nil {
		return Status{State: "idle"}
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.active != nil {
		return manager.active.status
	}
	return manager.last
}

func (manager *Manager) Close() error {
	if manager == nil {
		return nil
	}
	manager.mu.Lock()
	running := manager.active != nil
	manager.mu.Unlock()
	if !running {
		return nil
	}
	_, err := manager.Stop()
	return err
}

// Region applies stable, non-secret labels until the returned restore function runs.
func Region(ctx context.Context, component string, correlation string) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	labelValues := []string{"component", sanitizeLabel(component)}
	if correlation = shortCorrelation(correlation); correlation != "" {
		labelValues = append(labelValues, "correlation", correlation)
	}
	labeledContext := pprof.WithLabels(ctx, pprof.Labels(labelValues...))
	pprof.SetGoroutineLabels(labeledContext)
	return labeledContext, func() {
		pprof.SetGoroutineLabels(ctx)
	}
}

// Do applies stable, non-secret labels while fn executes.
func Do(ctx context.Context, component string, correlation string, fn func(context.Context) error) error {
	labeledContext, restore := Region(ctx, component, correlation)
	defer restore()
	return fn(labeledContext)
}

func normalizeTraceDuration(value time.Duration) time.Duration {
	if value <= 0 {
		return defaultTraceDuration
	}
	if value < minTraceDuration {
		return minTraceDuration
	}
	if value > maxTraceDuration {
		return maxTraceDuration
	}
	return value
}

func sanitizeLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '.' || character == '_' || character == '-' {
			builder.WriteRune(character)
		}
	}
	if builder.Len() == 0 {
		return "unknown"
	}
	return builder.String()
}

func shortCorrelation(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	// Request IDs can be user-controlled, so only retain a short, non-reversible digest.
	var hash uint64 = 14695981039346656037
	for index := 0; index < len(value); index++ {
		hash ^= uint64(value[index])
		hash *= 1099511628211
	}
	return fmt.Sprintf("%016x", hash)
}
