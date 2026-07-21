package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

type Store struct {
	path     string
	logsRoot string
	mu       sync.Mutex
}

type fileSnapshot struct {
	exists  bool
	modTime int64
	size    int64
}

func NewStore(path string, logsRoot string) *Store {
	return &Store{
		path:     strings.TrimSpace(path),
		logsRoot: strings.TrimSpace(logsRoot),
	}
}

func (store *Store) Path() string {
	if store == nil {
		return ""
	}
	return store.path
}

func (store *Store) LogsRoot() string {
	if store == nil {
		return ""
	}
	return store.logsRoot
}

func (store *Store) snapshot() fileSnapshot {
	if store == nil || strings.TrimSpace(store.path) == "" {
		return fileSnapshot{}
	}
	info, err := os.Stat(store.path)
	if err != nil {
		return fileSnapshot{}
	}
	return fileSnapshot{
		exists:  true,
		modTime: info.ModTime().UnixNano(),
		size:    info.Size(),
	}
}

func (store *Store) Load(_ context.Context) (Config, error) {
	if store == nil || strings.TrimSpace(store.path) == "" {
		return DefaultConfig(), nil
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	raw, current, exists, err := store.loadLocked()
	if err != nil {
		return DefaultConfig(), err
	}
	if !exists {
		if err := store.saveLocked(current); err != nil {
			return DefaultConfig(), err
		}
		return current, nil
	}
	if shouldPersistNormalizedConfig(raw, current, current) {
		if err := store.saveLocked(current); err != nil {
			return DefaultConfig(), err
		}
	}
	return current, nil
}

func (store *Store) Save(_ context.Context, cfg Config) (Config, error) {
	if store == nil || strings.TrimSpace(store.path) == "" {
		return Config{}, errors.New("配置存储未初始化")
	}

	normalized, err := NormalizeConfig(cfg)
	if err != nil {
		return Config{}, err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	if err := store.saveLocked(normalized); err != nil {
		return Config{}, err
	}
	return normalized, nil
}

// Patch updates only fields explicitly present in patch. The YAML document is
// edited by path so omitted credentials, internal state, comments, and unknown
// keys remain owned by their original writer.
func (store *Store) Patch(_ context.Context, patch ConfigPatch) (Config, error) {
	if store == nil || strings.TrimSpace(store.path) == "" {
		return Config{}, errors.New("配置存储未初始化")
	}
	if err := validateConfigPatch(patch); err != nil {
		return Config{}, err
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	raw, current, exists, err := store.loadLocked()
	if err != nil {
		return Config{}, err
	}
	normalized, err := ApplyConfigPatch(current, patch)
	if err != nil {
		return Config{}, err
	}
	paths := configPatchPaths(patch)
	if len(paths) == 0 {
		return normalized, nil
	}
	if !exists {
		if err := store.saveLocked(normalized); err != nil {
			return Config{}, err
		}
		return normalized, nil
	}
	if err := store.savePathsLocked(raw, normalized, paths); err != nil {
		return Config{}, err
	}
	return normalized, nil
}

func (store *Store) loadLocked() ([]byte, Config, bool, error) {
	raw, err := os.ReadFile(store.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, DefaultConfig(), false, nil
		}
		return nil, Config{}, false, fmt.Errorf("读取用户配置失败: %w", err)
	}

	var current Config
	if err := yaml.Unmarshal(raw, &current); err != nil {
		return nil, Config{}, true, fmt.Errorf("解析用户配置失败: %w", err)
	}
	normalized, err := NormalizeConfig(current)
	if err != nil {
		return nil, Config{}, true, err
	}
	return raw, normalized, true, nil
}

func (store *Store) saveLocked(normalized Config) error {
	raw, err := os.ReadFile(store.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取用户配置失败: %w", err)
	}
	data, err := mergeConfigYAML(raw, normalized, nil)
	if err != nil {
		return err
	}
	return store.writeLocked(data)
}

func (store *Store) savePathsLocked(raw []byte, normalized Config, paths []string) error {
	data, err := mergeConfigYAML(raw, normalized, paths)
	if err != nil {
		return err
	}
	return store.writeLocked(data)
}

func (store *Store) writeLocked(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(store.path), 0o755); err != nil {
		return fmt.Errorf("创建用户配置目录失败: %w", err)
	}
	tempPath := store.path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return fmt.Errorf("写入临时配置失败: %w", err)
	}
	if err := os.Rename(tempPath, store.path); err != nil {
		return fmt.Errorf("保存用户配置失败: %w", err)
	}
	return nil
}

func mergeConfigYAML(raw []byte, normalized Config, paths []string) ([]byte, error) {
	var source yaml.Node
	normalizedData, err := yaml.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("序列化用户配置失败: %w", err)
	}
	if err := yaml.Unmarshal(normalizedData, &source); err != nil {
		return nil, fmt.Errorf("构建用户配置节点失败: %w", err)
	}
	if len(source.Content) == 0 {
		return nil, errors.New("构建用户配置节点失败: empty document")
	}

	if len(raw) == 0 {
		return normalizedData, nil
	}
	var destination yaml.Node
	if err := yaml.Unmarshal(raw, &destination); err != nil {
		return nil, fmt.Errorf("解析用户配置失败: %w", err)
	}
	if len(destination.Content) == 0 || destination.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("解析用户配置失败: root must be a mapping")
	}

	if len(paths) == 0 {
		mergeYAMLNode(destination.Content[0], source.Content[0])
	} else {
		for _, path := range paths {
			if err := copyYAMLPath(destination.Content[0], source.Content[0], strings.Split(path, ".")); err != nil {
				return nil, err
			}
		}
	}
	data, err := yaml.Marshal(&destination)
	if err != nil {
		return nil, fmt.Errorf("序列化用户配置失败: %w", err)
	}
	return data, nil
}

func mergeYAMLNode(destination *yaml.Node, source *yaml.Node) {
	if destination == nil || source == nil {
		return
	}
	if destination.Kind != yaml.MappingNode || source.Kind != yaml.MappingNode {
		*destination = *cloneYAMLNode(source)
		return
	}
	for index := 0; index+1 < len(source.Content); index += 2 {
		key := source.Content[index]
		value := source.Content[index+1]
		_, destinationValue := mappingValue(destination, key.Value)
		if destinationValue == nil {
			destination.Content = append(destination.Content, cloneYAMLNode(key), cloneYAMLNode(value))
			continue
		}
		mergeYAMLNode(destinationValue, value)
	}
}

func copyYAMLPath(destination *yaml.Node, source *yaml.Node, path []string) error {
	if len(path) == 0 {
		return nil
	}
	if destination == nil || destination.Kind != yaml.MappingNode || source == nil || source.Kind != yaml.MappingNode {
		return fmt.Errorf("更新用户配置失败: invalid path %s", strings.Join(path, "."))
	}
	key := path[0]
	_, sourceValue := mappingValue(source, key)
	if sourceValue == nil {
		return fmt.Errorf("更新用户配置失败: unknown path %s", strings.Join(path, "."))
	}
	destinationIndex, destinationValue := mappingValue(destination, key)
	if len(path) == 1 {
		if destinationValue == nil {
			destination.Content = append(destination.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, cloneYAMLNode(sourceValue))
		} else {
			destination.Content[destinationIndex+1] = cloneYAMLNode(sourceValue)
		}
		return nil
	}
	if destinationValue == nil {
		destinationValue = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		destination.Content = append(destination.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, destinationValue)
	}
	return copyYAMLPath(destinationValue, sourceValue, path[1:])
}

func mappingValue(mapping *yaml.Node, key string) (int, *yaml.Node) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return -1, nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return index, mapping.Content[index+1]
		}
	}
	return -1, nil
}

func cloneYAMLNode(source *yaml.Node) *yaml.Node {
	if source == nil {
		return nil
	}
	clone := *source
	clone.Content = make([]*yaml.Node, len(source.Content))
	for index, child := range source.Content {
		clone.Content[index] = cloneYAMLNode(child)
	}
	return &clone
}

func validateConfigPatch(patch ConfigPatch) error {
	if patch.ProviderStreamIdleTimeout != nil && *patch.ProviderStreamIdleTimeout < MinProviderStreamIdleTimeoutSeconds {
		return fmt.Errorf("providerStreamIdleTimeout must be at least %d", MinProviderStreamIdleTimeoutSeconds)
	}
	if patch.HomeMetrics != nil && patch.HomeMetrics.RefreshIntervalSeconds != nil && *patch.HomeMetrics.RefreshIntervalSeconds < 1 {
		return errors.New("homeMetrics.refreshIntervalSeconds must be at least 1")
	}
	return nil
}

func configPatchPaths(patch ConfigPatch) []string {
	paths := make([]string, 0, 10)
	if patch.Log != nil {
		paths = append(paths, "log")
	}
	if patch.DisableUpdates != nil {
		paths = append(paths, "disableUpdates")
	}
	if patch.CompactContextTools != nil {
		paths = append(paths, "compactContextTools")
	}
	if patch.ResponseLanguage != nil {
		paths = append(paths, "responseLanguage")
	}
	if patch.ProviderStreamIdleTimeout != nil {
		paths = append(paths, "providerStreamIdleTimeout")
	}
	if patch.BackendListenAddr != nil {
		paths = append(paths, "backendListenAddr")
	}
	if patch.ProxyListenAddr != nil {
		paths = append(paths, "proxyListenAddr")
	}
	if patch.Routing != nil && patch.Routing.Mode != nil {
		paths = append(paths, "routing.mode")
	}
	if patch.HomeMetrics != nil {
		if patch.HomeMetrics.IncludeCacheWriteInHitRate != nil {
			paths = append(paths, "homeMetrics.includeCacheWriteInHitRate")
		}
		if patch.HomeMetrics.RefreshIntervalSeconds != nil {
			paths = append(paths, "homeMetrics.refreshIntervalSeconds")
		}
	}
	return paths
}

func shouldPersistNormalizedConfig(raw []byte, current Config, normalized Config) bool {
	if !yamlHasKey(raw, "disableUpdates") || !yamlHasKey(raw, "responseLanguage") || !yamlHasKey(raw, "backendListenAddr") || !yamlHasKey(raw, "proxyListenAddr") {
		return true
	}
	if current.BackendListenAddr != normalized.BackendListenAddr || current.ProxyListenAddr != normalized.ProxyListenAddr {
		return true
	}
	if current.ResponseLanguage != normalized.ResponseLanguage || current.HomeMetrics.RefreshIntervalSeconds != normalized.HomeMetrics.RefreshIntervalSeconds {
		return true
	}
	if current.ProviderStreamIdleTimeout == normalized.ProviderStreamIdleTimeout {
		return false
	}
	return yamlHasKey(raw, "providerStreamIdleTimeout")
}

func yamlHasKey(raw []byte, key string) bool {
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return false
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return false
	}
	mapping := root.Content[0]
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return true
		}
	}
	return false
}
