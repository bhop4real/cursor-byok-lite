package bridge

import (
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"sync"

	"cursor/internal/buildinfo"
	"cursor/internal/client"
	"cursor/internal/updater"

	"github.com/leaanthony/u"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const maxWindowTitleLength = 160

// modelEditorContext 保存当前模型编辑器窗口的初始化上下文。
type modelEditorContext struct {
	Index       int    `json:"index"`
	AdapterJSON string `json:"adapterJSON"`
}

// WindowTitles contains frontend-localized titles for known native windows.
type WindowTitles struct {
	Main            string `json:"main"`
	Config          string `json:"config"`
	ModelConfig     string `json:"modelConfig"`
	ModelEditorAdd  string `json:"modelEditorAdd"`
	ModelEditorEdit string `json:"modelEditorEdit"`
}

// WindowService 定义了当前模块中的 WindowService 类型。
type WindowService struct {
	app               *application.App
	updater           *updater.Manager
	titles            WindowTitles
	configWindow      *application.WebviewWindow
	modelConfigWindow *application.WebviewWindow
	modelEditorWindow *application.WebviewWindow
	editorCtx         *modelEditorContext
	mu                sync.RWMutex
}

// NewWindowService 用于处理与 NewWindowService 相关的逻辑。
func NewWindowService() *WindowService {
	return &WindowService{titles: defaultWindowTitles()}
}

func defaultWindowTitles() WindowTitles {
	return WindowTitles{
		Main:            "Cursor 助手",
		Config:          "设置",
		ModelConfig:     "模型配置",
		ModelEditorAdd:  "新增模型配置",
		ModelEditorEdit: "编辑模型配置",
	}
}

// SetApp 用于处理与 SetApp 相关的逻辑。
func (s *WindowService) SetApp(app *application.App) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.app = app
}

// SetUpdater 关联更新管理器，供前端手动触发检查更新。
func (s *WindowService) SetUpdater(manager *updater.Manager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updater = manager
}

// GetAppVersion 返回当前应用版本号。
func (s *WindowService) GetAppVersion() string {
	return buildinfo.CurrentVersion()
}

// CheckForUpdates 触发一次手动检查更新。
func (s *WindowService) CheckForUpdates() error {
	s.mu.RLock()
	manager := s.updater
	s.mu.RUnlock()
	if manager == nil {
		return fmt.Errorf("更新管理器未初始化")
	}
	return manager.CheckNow(true)
}

// InstallReadyUpdate 安装当前已下载完成的更新。
func (s *WindowService) InstallReadyUpdate() error {
	s.mu.RLock()
	manager := s.updater
	s.mu.RUnlock()
	if manager == nil {
		return fmt.Errorf("更新管理器未初始化")
	}
	return manager.InstallReadyUpdate()
}

// SetWindowTitle updates only the native window identified by the caller.
func (s *WindowService) SetWindowTitle(windowID uint, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("window title is required")
	}
	if len([]rune(title)) > maxWindowTitleLength {
		return fmt.Errorf("window title must not exceed %d characters", maxWindowTitleLength)
	}

	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	if app == nil {
		return fmt.Errorf("application is not initialized")
	}
	window, ok := app.Window.GetByID(windowID)
	if !ok {
		return fmt.Errorf("window %d does not exist", windowID)
	}
	window.SetTitle(title)
	return nil
}

// UpdateWindowTitles stores localized defaults and refreshes open subwindows.
func (s *WindowService) UpdateWindowTitles(titles WindowTitles) error {
	normalized, err := normalizeWindowTitles(titles)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.titles = normalized
	if s.configWindow != nil {
		s.configWindow.SetTitle(normalized.Config)
	}
	if s.modelConfigWindow != nil {
		s.modelConfigWindow.SetTitle(normalized.ModelConfig)
	}
	if s.modelEditorWindow != nil {
		title := normalized.ModelEditorAdd
		if s.editorCtx != nil && s.editorCtx.Index >= 0 {
			title = normalized.ModelEditorEdit
		}
		s.modelEditorWindow.SetTitle(title)
	}
	s.mu.Unlock()
	return nil
}

func normalizeWindowTitles(titles WindowTitles) (WindowTitles, error) {
	defaults := defaultWindowTitles()
	values := []*string{
		&titles.Main,
		&titles.Config,
		&titles.ModelConfig,
		&titles.ModelEditorAdd,
		&titles.ModelEditorEdit,
	}
	fallbacks := []string{
		defaults.Main,
		defaults.Config,
		defaults.ModelConfig,
		defaults.ModelEditorAdd,
		defaults.ModelEditorEdit,
	}
	for index, value := range values {
		*value = strings.TrimSpace(*value)
		if *value == "" {
			*value = fallbacks[index]
		}
		if len([]rune(*value)) > maxWindowTitleLength {
			return WindowTitles{}, fmt.Errorf("window title must not exceed %d characters", maxWindowTitleLength)
		}
	}
	return titles, nil
}

// OpenConfigWindow 打开设置窗口。如果窗口已存在则聚焦。
func (s *WindowService) OpenConfigWindow() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.app == nil {
		return
	}
	if s.configWindow != nil {
		s.configWindow.Show()
		s.configWindow.Focus()
		return
	}

	win := s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:               s.titles.Config,
		Width:               820,
		Height:              680,
		MinWidth:            700,
		MinHeight:           560,
		DisableResize:       false,
		Frameless:           goruntime.GOOS == "windows",
		URL:                 "/#/config",
		Hidden:              false,
		HideOnEscape:        false,
		MinimiseButtonState: application.ButtonEnabled,
		MaximiseButtonState: application.ButtonEnabled,
		CloseButtonState:    application.ButtonEnabled,
		BackgroundColour:    application.RGBA{Red: 25, Green: 25, Blue: 25, Alpha: 255},
		Mac: application.MacWindow{
			Backdrop:      application.MacBackdropLiquidGlass,
			DisableShadow: false,
			TitleBar: application.MacTitleBar{
				AppearsTransparent:   true,
				Hide:                 false,
				HideTitle:            true,
				FullSizeContent:      true,
				UseToolbar:           false,
				HideToolbarSeparator: true,
			},
			WebviewPreferences: application.MacWebviewPreferences{
				FullscreenEnabled:                   u.True,
				TextInteractionEnabled:              u.True,
				AllowsBackForwardNavigationGestures: u.False,
			},
		},
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: false,
		},
	})
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.configWindow = nil
	})
	s.configWindow = win
}

// OpenSettingsDirectory 打开本地设置目录。
func (s *WindowService) OpenSettingsDirectory() {
	_ = os.MkdirAll(client.ResolveSettingsRootPath(), 0o755)
	openDirectory(client.ResolveSettingsRootPath())
}

// OpenModelConfigWindow 打开模型配置独立窗口。如果窗口已存在则聚焦。
func (s *WindowService) OpenModelConfigWindow() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.app == nil {
		return
	}

	if s.modelConfigWindow != nil {
		s.modelConfigWindow.Show()
		s.modelConfigWindow.Focus()
		return
	}

	win := s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:               s.titles.ModelConfig,
		Width:               980,
		Height:              700,
		MinWidth:            820,
		MinHeight:           560,
		DisableResize:       false,
		Frameless:           goruntime.GOOS == "windows",
		URL:                 "/#/model-config",
		Hidden:              false,
		HideOnEscape:        false,
		MinimiseButtonState: application.ButtonEnabled,
		MaximiseButtonState: application.ButtonEnabled,
		CloseButtonState:    application.ButtonEnabled,
		BackgroundColour:    application.RGBA{Red: 25, Green: 25, Blue: 25, Alpha: 255},
		Mac: application.MacWindow{
			Backdrop:      application.MacBackdropLiquidGlass,
			DisableShadow: false,
			TitleBar: application.MacTitleBar{
				AppearsTransparent:   true,
				Hide:                 false,
				HideTitle:            true,
				FullSizeContent:      true,
				UseToolbar:           false,
				HideToolbarSeparator: true,
			},
			WebviewPreferences: application.MacWebviewPreferences{
				FullscreenEnabled:                   u.True,
				TextInteractionEnabled:              u.True,
				AllowsBackForwardNavigationGestures: u.False,
			},
		},
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: false,
		},
	})

	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.modelConfigWindow = nil
	})

	s.modelConfigWindow = win
}

// OpenModelEditorWindow 打开模型编辑器独立窗口。
// index < 0 表示新增，>= 0 表示编辑对应索引的适配器。
// adapterJSON 为编辑器初始数据的 JSON 字符串。
func (s *WindowService) OpenModelEditorWindow(index int, adapterJSON string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.app == nil {
		return
	}

	s.editorCtx = &modelEditorContext{
		Index:       index,
		AdapterJSON: adapterJSON,
	}

	if s.modelEditorWindow != nil {
		title := s.titles.ModelEditorAdd
		if index >= 0 {
			title = s.titles.ModelEditorEdit
		}
		s.modelEditorWindow.SetTitle(title)
		s.modelEditorWindow.Show()
		s.modelEditorWindow.Focus()
		return
	}

	title := s.titles.ModelEditorAdd
	if index >= 0 {
		title = s.titles.ModelEditorEdit
	}

	win := s.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:               title,
		Width:               840,
		Height:              680,
		MinWidth:            740,
		MinHeight:           600,
		DisableResize:       false,
		Frameless:           goruntime.GOOS == "windows",
		URL:                 fmt.Sprintf("/#/model-editor?index=%d", index),
		Hidden:              false,
		HideOnEscape:        false,
		MinimiseButtonState: application.ButtonEnabled,
		MaximiseButtonState: application.ButtonEnabled,
		CloseButtonState:    application.ButtonEnabled,
		BackgroundColour:    application.RGBA{Red: 25, Green: 25, Blue: 25, Alpha: 255},
		Mac: application.MacWindow{
			Backdrop:      application.MacBackdropLiquidGlass,
			DisableShadow: false,
			TitleBar: application.MacTitleBar{
				AppearsTransparent:   true,
				Hide:                 false,
				HideTitle:            true,
				FullSizeContent:      true,
				UseToolbar:           false,
				HideToolbarSeparator: true,
			},
			WebviewPreferences: application.MacWebviewPreferences{
				FullscreenEnabled:                   u.False,
				TextInteractionEnabled:              u.True,
				AllowsBackForwardNavigationGestures: u.False,
			},
		},
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: false,
		},
	})

	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.modelEditorWindow = nil
		s.editorCtx = nil
	})

	s.modelEditorWindow = win
}

// GetModelEditorContext 返回当前编辑器窗口的初始化上下文。
func (s *WindowService) GetModelEditorContext() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.editorCtx == nil {
		return map[string]any{
			"index":       -1,
			"adapterJSON": "{}",
		}
	}
	return map[string]any{
		"index":       s.editorCtx.Index,
		"adapterJSON": s.editorCtx.AdapterJSON,
	}
}

// OpenHistoryWindow 用于处理与 OpenHistoryWindow 相关的逻辑。
func (s *WindowService) OpenHistoryWindow() {
	_ = os.MkdirAll(client.ResolveLogsRootPath(), 0o755)
	openDirectory(client.ResolveLogsRootPath())
}

// openDirectory 用于处理与 openDirectory 相关的逻辑。
func openDirectory(path string) {
	if path == "" {
		return
	}
	switch goruntime.GOOS {
	case "darwin":
		_ = exec.Command("open", path).Start()
	case "windows":
		_ = exec.Command("explorer", path).Start()
	default:
		_ = exec.Command("xdg-open", path).Start()
	}
}
