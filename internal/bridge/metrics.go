package bridge

import (
	"cursor/internal/appdata"
	"cursor/internal/historymetrics"
)

// HomeMetricsHourlyPoint 定义首页 24 小时趋势的整点统计。
type HomeMetricsHourlyPoint struct {
	At                 string `json:"at"`
	TurnsTotal         int    `json:"turnsTotal"`
	RequestTokensTotal int64  `json:"requestTokensTotal"`
	PromptTokensTotal  int64  `json:"promptTokensTotal"`
	InputTokens        int64  `json:"inputTokens"`
	OutputTokens       int64  `json:"outputTokens"`
	CacheReadTokens    int64  `json:"cacheReadTokens"`
	CacheWriteTokens   int64  `json:"cacheWriteTokens"`
}

// HomeMetricsSummary 定义首页展示的历史统计摘要。
type HomeMetricsSummary struct {
	ProviderCallsTotal int                      `json:"providerCallsTotal"`
	TurnsTotal         int                      `json:"turnsTotal"`
	ValidTurnsTotal    int                      `json:"validTurnsTotal"`
	InvalidTurnsTotal  int                      `json:"invalidTurnsTotal"`
	RequestTokensTotal int64                    `json:"requestTokensTotal"`
	PromptTokensTotal  int64                    `json:"promptTokensTotal"`
	CacheReadTokens    int64                    `json:"cacheReadTokens"`
	CacheWriteTokens   int64                    `json:"cacheWriteTokens"`
	CacheHitRate       *float64                 `json:"cacheHitRate"`
	Last24Hours        []HomeMetricsHourlyPoint `json:"last24Hours"`
}

// MetricsService 定义首页统计相关的 Wails service。
type MetricsService struct{}

// NewMetricsService 创建首页统计 service。
func NewMetricsService() *MetricsService {
	return &MetricsService{}
}

// GetHomeMetricsSummary 返回首页展示的全量历史统计摘要。
func (service *MetricsService) GetHomeMetricsSummary() (HomeMetricsSummary, error) {
	if err := appdata.EnsureAssistantHome(); err != nil {
		return HomeMetricsSummary{}, err
	}

	summary, err := historymetrics.LoadUsageSummary(appdata.UsageFilePath())
	if err != nil {
		return HomeMetricsSummary{}, err
	}
	return HomeMetricsSummary{
		ProviderCallsTotal: summary.ProviderCallsTotal,
		TurnsTotal:         summary.TurnsTotal,
		ValidTurnsTotal:    summary.ValidTurnsTotal,
		InvalidTurnsTotal:  summary.InvalidTurnsTotal,
		RequestTokensTotal: summary.RequestTokensTotal,
		PromptTokensTotal:  summary.PromptTokensTotal,
		CacheReadTokens:    summary.CacheReadTokens,
		CacheWriteTokens:   summary.CacheWriteTokens,
		CacheHitRate:       summary.CacheHitRate,
		Last24Hours:        toHomeMetricsHourlyPoints(summary.Last24Hours),
	}, nil
}

func toHomeMetricsHourlyPoints(source []historymetrics.HourlyPoint) []HomeMetricsHourlyPoint {
	points := make([]HomeMetricsHourlyPoint, 0, len(source))
	for _, point := range source {
		points = append(points, HomeMetricsHourlyPoint{
			At:                 point.At,
			TurnsTotal:         point.TurnsTotal,
			RequestTokensTotal: point.RequestTokensTotal,
			PromptTokensTotal:  point.PromptTokensTotal,
			InputTokens:        point.InputTokens,
			OutputTokens:       point.OutputTokens,
			CacheReadTokens:    point.CacheReadTokens,
			CacheWriteTokens:   point.CacheWriteTokens,
		})
	}
	return points
}
