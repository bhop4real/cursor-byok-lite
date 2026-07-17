package historymetrics

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	hourlyTrendPointCount = 24
	usageEventKindTurn    = "turn_finalized"
)

type usageFileDocument struct {
	Totals struct {
		ProviderCalls     int64 `json:"provider_calls"`
		TurnsTotal        int64 `json:"turns_total"`
		ValidTurnsTotal   int64 `json:"valid_turns_total"`
		InvalidTurnsTotal int64 `json:"invalid_turns_total"`
		InputTokens       int64 `json:"input_tokens"`
		OutputTokens      int64 `json:"output_tokens"`
		CacheReadTokens   int64 `json:"cache_read_tokens"`
		CacheWriteTokens  int64 `json:"cache_write_tokens"`
		TotalTokens       int64 `json:"total_tokens"`
	} `json:"totals"`
	Hourly       []usageFileHourly `json:"hourly"`
	RecentEvents []usageFileEvent  `json:"recent_events"`
}

type usageFileHourly struct {
	Hour             string `json:"hour"`
	TurnsTotal       int64  `json:"turns_total"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
}

type usageFileEvent struct {
	Kind             string    `json:"kind"`
	At               time.Time `json:"at"`
	InputTokens      int64     `json:"input_tokens"`
	OutputTokens     int64     `json:"output_tokens"`
	CacheReadTokens  int64     `json:"cache_read_tokens"`
	CacheWriteTokens int64     `json:"cache_write_tokens"`
}

func LoadUsageSummary(path string) (Summary, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Summary{Last24Hours: buildHourlyTrend(nil, nil, time.Now())}, nil
		}
		return Summary{}, fmt.Errorf("read usage file: %w", err)
	}
	var doc usageFileDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return Summary{}, fmt.Errorf("decode usage file: %w", err)
	}
	totals := Totals{
		InputTokens:        doc.Totals.InputTokens,
		OutputTokens:       doc.Totals.OutputTokens,
		CacheReadTokens:    doc.Totals.CacheReadTokens,
		CacheWriteTokens:   doc.Totals.CacheWriteTokens,
		PromptTokensTotal:  doc.Totals.InputTokens + doc.Totals.CacheReadTokens + doc.Totals.CacheWriteTokens,
		RequestTokensTotal: doc.Totals.TotalTokens,
	}
	return Summary{
		ProviderCallsTotal: int(doc.Totals.ProviderCalls),
		TurnsTotal:         int(doc.Totals.TurnsTotal),
		ValidTurnsTotal:    int(doc.Totals.ValidTurnsTotal),
		InvalidTurnsTotal:  int(doc.Totals.InvalidTurnsTotal),
		RequestTokensTotal: totals.RequestTokensTotal,
		PromptTokensTotal:  totals.PromptTokensTotal,
		CacheReadTokens:    totals.CacheReadTokens,
		CacheWriteTokens:   totals.CacheWriteTokens,
		CacheHitRate:       cacheHitRateFromTotals(totals),
		Last24Hours:        buildHourlyTrend(doc.Hourly, doc.RecentEvents, time.Now()),
	}, nil
}

func buildHourlyTrend(hourly []usageFileHourly, events []usageFileEvent, now time.Time) []HourlyPoint {
	endHour := now.UTC().Truncate(time.Hour)
	startHour := endHour.Add(-time.Duration(hourlyTrendPointCount-1) * time.Hour)
	points := make([]HourlyPoint, hourlyTrendPointCount)
	for index := range points {
		points[index].At = startHour.Add(time.Duration(index) * time.Hour).Format(time.RFC3339)
	}

	if len(hourly) > 0 {
		for _, bucket := range hourly {
			bucketHour, err := time.Parse(time.RFC3339, strings.TrimSpace(bucket.Hour))
			if err != nil {
				continue
			}
			bucketHour = bucketHour.UTC().Truncate(time.Hour)
			if bucketHour.Before(startHour) || bucketHour.After(endHour) {
				continue
			}
			point := &points[int(bucketHour.Sub(startHour)/time.Hour)]
			point.TurnsTotal = int(nonNegative(bucket.TurnsTotal))
			point.InputTokens = nonNegative(bucket.InputTokens)
			point.OutputTokens = nonNegative(bucket.OutputTokens)
			point.CacheReadTokens = nonNegative(bucket.CacheReadTokens)
			point.CacheWriteTokens = nonNegative(bucket.CacheWriteTokens)
			point.PromptTokensTotal = point.InputTokens + point.CacheReadTokens + point.CacheWriteTokens
			point.RequestTokensTotal = point.PromptTokensTotal + point.OutputTokens
		}
		return points
	}

	for _, event := range events {
		eventHour := event.At.UTC().Truncate(time.Hour)
		if event.At.IsZero() || eventHour.Before(startHour) || eventHour.After(endHour) {
			continue
		}
		index := int(eventHour.Sub(startHour) / time.Hour)
		point := &points[index]
		if strings.TrimSpace(event.Kind) == usageEventKindTurn {
			point.TurnsTotal++
			continue
		}
		point.InputTokens += nonNegative(event.InputTokens)
		point.OutputTokens += nonNegative(event.OutputTokens)
		point.CacheReadTokens += nonNegative(event.CacheReadTokens)
		point.CacheWriteTokens += nonNegative(event.CacheWriteTokens)
		point.PromptTokensTotal = point.InputTokens + point.CacheReadTokens + point.CacheWriteTokens
		point.RequestTokensTotal = point.PromptTokensTotal + point.OutputTokens
	}
	return points
}

func nonNegative(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
