package client

import (
	"context"
	"errors"
	"fmt"

	"cursor/internal/appdata"
	serverconfig "cursor/internal/backend/server/config"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// UserConfig 定义了当前模块中的 UserConfig 类型。
type UserConfig = serverconfig.Config

// UserConfigPatch contains only settings explicitly changed by the UI.
type UserConfigPatch = serverconfig.ConfigPatch

// EditableConfigMetadata describes backend-owned settings constraints.
type EditableConfigMetadata = serverconfig.EditableConfigMetadata

// GetEditableConfigMetadata returns the public settings contract.
func (s *ProxyService) GetEditableConfigMetadata() EditableConfigMetadata {
	return serverconfig.EditableConfigContract()
}

// LoadUserConfig 用于处理与 LoadUserConfig 相关的逻辑。
func (s *ProxyService) LoadUserConfig() (UserConfig, error) {
	if s == nil {
		return serverconfig.DefaultConfig(), nil
	}
	ctx := applicationContext()
	if s.backendHost != nil {
		return s.backendHost.LoadConfig(ctx)
	}
	if s.store == nil {
		return serverconfig.DefaultConfig(), nil
	}
	return s.store.Load(ctx)
}

// SaveUserConfig remains the full-save path for the dedicated model editor.
func (s *ProxyService) SaveUserConfig(cfg UserConfig) error {
	if s == nil {
		return nil
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()

	normalized, err := s.saveUserConfig(applicationContext(), cfg)
	if err != nil {
		return err
	}
	s.emitUserConfigChanged(normalized)
	return nil
}

// PatchUserConfig applies only user-owned settings and preserves omitted
// credentials, internal values, comments, and unknown YAML fields.
func (s *ProxyService) PatchUserConfig(patch UserConfigPatch) (UserConfig, error) {
	if s == nil {
		return serverconfig.DefaultConfig(), nil
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()

	ctx := applicationContext()
	previous, err := s.loadUserConfig(ctx)
	if err != nil {
		return UserConfig{}, err
	}
	wasRunning := s.GetState().BackendRunning || s.GetState().ProxyRunning
	normalized, err := s.patchUserConfig(ctx, patch)
	if err != nil {
		return UserConfig{}, err
	}

	listenersChanged := previous.BackendListenAddr != normalized.BackendListenAddr ||
		previous.ProxyListenAddr != normalized.ProxyListenAddr
	if !listenersChanged || !wasRunning {
		s.emitUserConfigChanged(normalized)
		return normalized, nil
	}

	if _, err := s.StopProxy(); err == nil {
		if _, startErr := s.StartProxy(); startErr == nil {
			s.emitUserConfigChanged(normalized)
			return normalized, nil
		} else {
			err = startErr
		}
		return s.rollbackListenerPatch(ctx, previous, fmt.Errorf("重启服务以应用监听地址失败: %w", err))
	} else {
		return s.rollbackListenerPatch(ctx, previous, fmt.Errorf("停止服务以应用监听地址失败: %w", err))
	}
}

func (s *ProxyService) rollbackListenerPatch(ctx context.Context, previous UserConfig, applyErr error) (UserConfig, error) {
	rollbackPatch := inverseListenerConfigPatch(previous)
	restored, rollbackErr := s.patchUserConfig(ctx, rollbackPatch)
	if rollbackErr != nil {
		return UserConfig{}, errors.Join(applyErr, fmt.Errorf("回滚监听地址配置失败: %w", rollbackErr))
	}

	_, stopErr := s.StopProxy()
	_, startErr := s.StartProxy()
	s.emitUserConfigChanged(restored)
	if stopErr != nil || startErr != nil {
		return restored, errors.Join(
			applyErr,
			fmt.Errorf("配置已回滚，但恢复原服务失败: %w", errors.Join(stopErr, startErr)),
		)
	}
	return restored, applyErr
}

func (s *ProxyService) loadUserConfig(ctx context.Context) (UserConfig, error) {
	if s.backendHost != nil {
		return s.backendHost.LoadConfig(ctx)
	}
	if s.store == nil {
		return serverconfig.DefaultConfig(), nil
	}
	return s.store.Load(ctx)
}

func (s *ProxyService) saveUserConfig(ctx context.Context, cfg UserConfig) (UserConfig, error) {
	if s.backendHost != nil {
		return s.backendHost.SaveConfig(ctx, cfg)
	}
	if s.store != nil {
		return s.store.Save(ctx, cfg)
	}
	return serverconfig.DefaultConfig(), nil
}

func (s *ProxyService) patchUserConfig(ctx context.Context, patch UserConfigPatch) (UserConfig, error) {
	if s.backendHost != nil {
		return s.backendHost.PatchConfig(ctx, patch)
	}
	if s.store != nil {
		return s.store.Patch(ctx, patch)
	}
	return serverconfig.DefaultConfig(), nil
}

func inverseListenerConfigPatch(previous UserConfig) UserConfigPatch {
	backendListenAddr := previous.BackendListenAddr
	proxyListenAddr := previous.ProxyListenAddr
	return UserConfigPatch{
		BackendListenAddr: &backendListenAddr,
		ProxyListenAddr:   &proxyListenAddr,
	}
}

func applicationContext() context.Context {
	app := application.Get()
	if app != nil {
		return app.Context()
	}
	return context.Background()
}

func (s *ProxyService) emitUserConfigChanged(cfg UserConfig) {
	app := application.Get()
	if app == nil {
		return
	}
	app.Event.Emit("user-config:changed", cfg)
}

// resolveUserConfigPath 用于处理与 resolveUserConfigPath 相关的逻辑。
func resolveUserConfigPath() string {
	return appdata.ConfigFilePath()
}

// resolveLogsRootPath 用于处理与 resolveLogsRootPath 相关的逻辑。
func resolveLogsRootPath() string {
	return appdata.LogsRootPath()
}

// ResolveLogsRootPath 用于处理与 ResolveLogsRootPath 相关的逻辑。
func ResolveLogsRootPath() string {
	return resolveLogsRootPath()
}

// ResolveSettingsRootPath 用于处理与 ResolveSettingsRootPath 相关的逻辑。
func ResolveSettingsRootPath() string {
	return appdata.RootDir()
}
