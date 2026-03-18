package observability

import (
	"strings"
	"sync"
	"time"
)

const (
	MetricSyncJobDurationSeconds  = "sync_job_duration_seconds"
	MetricIndexJobDurationSeconds = "index_job_duration_seconds"
	MetricJobQueueLatencySeconds  = "indexsync_job_queue_latency_seconds"
	MetricDeltaSize               = "indexsync_delta_size"
	MetricJobFailuresTotal        = "indexsync_job_failures_total"
	MetricJobRetriesTotal         = "indexsync_job_retries_total"
	MetricJobCompletionsTotal     = "indexsync_job_completions_total"
)

type IndexSyncMetricEvent struct {
	Metric     string
	Kind       string
	Status     string
	SessionID  string
	RepoRoot   string
	JobID      int64
	Attempt    int
	SnapshotID int64
	RootHash   string
	DeltaSize  int
	Value      float64
	RecordedAt time.Time
}

type MetricsSnapshot struct {
	Counters map[string]float64
	Events   []IndexSyncMetricEvent
}

type IndexSyncMetrics struct {
	mu       sync.Mutex
	counters map[string]float64
	events   []IndexSyncMetricEvent
}

func NewIndexSyncMetrics() *IndexSyncMetrics {
	return &IndexSyncMetrics{
		counters: make(map[string]float64),
	}
}

func (m *IndexSyncMetrics) RecordJobLifecycle(event IndexSyncMetricEvent, queueLatency, duration time.Duration) {
	if m == nil {
		return
	}

	now := time.Now().UTC()
	if queueLatency < 0 {
		queueLatency = 0
	}
	if duration < 0 {
		duration = 0
	}

	base := normalizeMetricEvent(event, now)
	events := []IndexSyncMetricEvent{
		withMetricValue(base, MetricJobQueueLatencySeconds, queueLatency.Seconds(), now),
		withMetricValue(base, durationMetric(base.Kind), duration.Seconds(), now),
		withMetricValue(base, MetricDeltaSize, float64(max(event.DeltaSize, 0)), now),
	}

	statusMetric := MetricJobCompletionsTotal
	switch strings.TrimSpace(base.Status) {
	case "retry":
		statusMetric = MetricJobRetriesTotal
	case "failure":
		statusMetric = MetricJobFailuresTotal
	}
	events = append(events, withMetricValue(base, statusMetric, 1, now))

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, metricEvent := range events {
		m.counters[counterKey(metricEvent)] += metricEvent.Value
		m.events = append(m.events, metricEvent)
	}
}

func (m *IndexSyncMetrics) Snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{
			Counters: map[string]float64{},
			Events:   nil,
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	counters := make(map[string]float64, len(m.counters))
	for key, value := range m.counters {
		counters[key] = value
	}
	events := append([]IndexSyncMetricEvent(nil), m.events...)
	return MetricsSnapshot{
		Counters: counters,
		Events:   events,
	}
}

func (m *IndexSyncMetrics) Reset() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters = make(map[string]float64)
	m.events = nil
}

var defaultIndexSyncMetrics = NewIndexSyncMetrics()

func DefaultIndexSyncMetrics() *IndexSyncMetrics {
	return defaultIndexSyncMetrics
}

func durationMetric(kind string) string {
	switch strings.TrimSpace(kind) {
	case "index_apply_delta":
		return MetricIndexJobDurationSeconds
	default:
		return MetricSyncJobDurationSeconds
	}
}

func normalizeMetricEvent(event IndexSyncMetricEvent, now time.Time) IndexSyncMetricEvent {
	event.Kind = strings.TrimSpace(event.Kind)
	event.Status = strings.TrimSpace(event.Status)
	event.SessionID = strings.TrimSpace(event.SessionID)
	event.RepoRoot = strings.TrimSpace(event.RepoRoot)
	event.RootHash = strings.TrimSpace(event.RootHash)
	if event.RecordedAt.IsZero() {
		event.RecordedAt = now
	}
	return event
}

func withMetricValue(event IndexSyncMetricEvent, metric string, value float64, recordedAt time.Time) IndexSyncMetricEvent {
	event.Metric = metric
	event.Value = value
	event.RecordedAt = recordedAt
	return event
}

func counterKey(event IndexSyncMetricEvent) string {
	return strings.Join([]string{
		strings.TrimSpace(event.Metric),
		strings.TrimSpace(event.Kind),
		strings.TrimSpace(event.Status),
	}, "|")
}

func max(value, floor int) int {
	if value < floor {
		return floor
	}
	return value
}
