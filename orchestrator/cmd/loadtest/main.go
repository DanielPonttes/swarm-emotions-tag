package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type summary struct {
	BaseURL      string  `json:"base_url"`
	DurationSec  float64 `json:"duration_sec"`
	RPS          int     `json:"rps"`
	Agents       int     `json:"agents"`
	MaxInflight  int     `json:"max_inflight"`
	Total        int64   `json:"total"`
	Success      int64   `json:"success"`
	Failure      int64   `json:"failure"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	P95LatencyMs float64 `json:"p95_latency_ms"`
	MaxLatencyMs float64 `json:"max_latency_ms"`
	SampleError  string  `json:"sample_error,omitempty"`
}

func main() {
	var (
		baseURL        string
		duration       time.Duration
		requestTimeout time.Duration
		rps            int
		agents         int
		maxInflight    int
	)

	flag.StringVar(&baseURL, "base-url", "http://127.0.0.1:8080", "orchestrator base URL")
	flag.DurationVar(&duration, "duration", 30*time.Second, "load duration")
	flag.DurationVar(&requestTimeout, "timeout", 3*time.Second, "per-request timeout")
	flag.IntVar(&rps, "rps", 10, "target requests per second")
	flag.IntVar(&agents, "agents", 8, "number of rotating agent IDs")
	flag.IntVar(&maxInflight, "max-inflight", 64, "maximum concurrent requests")
	flag.Parse()

	if err := validateConfig(duration, requestTimeout, rps, agents, maxInflight); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	client := &http.Client{Timeout: requestTimeout}
	targetURL := strings.TrimRight(baseURL, "/") + "/api/v1/interact"
	interval := time.Second / time.Duration(rps)
	if interval <= 0 {
		interval = time.Nanosecond
	}

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var (
		totalCount   atomic.Int64
		successCount atomic.Int64
		failureCount atomic.Int64
		sequence     atomic.Uint64
		latencyMu    sync.Mutex
		latencies    = make([]time.Duration, 0, max(1, int(math.Ceil(duration.Seconds()*float64(rps)))))
		firstError   atomic.Value
		wg           sync.WaitGroup
		sem          = make(chan struct{}, maxInflight)
	)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			reportAndExit(summary{
				BaseURL:      strings.TrimRight(baseURL, "/"),
				DurationSec:  duration.Seconds(),
				RPS:          rps,
				Agents:       agents,
				MaxInflight:  maxInflight,
				Total:        totalCount.Load(),
				Success:      successCount.Load(),
				Failure:      failureCount.Load(),
				AvgLatencyMs: averageLatencyMs(latencies),
				P95LatencyMs: percentileLatencyMs(latencies, 0.95),
				MaxLatencyMs: maxLatencyMs(latencies),
				SampleError:  loadSampleError(&firstError),
			})
		case <-ticker.C:
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				wg.Wait()
				reportAndExit(summary{
					BaseURL:      strings.TrimRight(baseURL, "/"),
					DurationSec:  duration.Seconds(),
					RPS:          rps,
					Agents:       agents,
					MaxInflight:  maxInflight,
					Total:        totalCount.Load(),
					Success:      successCount.Load(),
					Failure:      failureCount.Load(),
					AvgLatencyMs: averageLatencyMs(latencies),
					P95LatencyMs: percentileLatencyMs(latencies, 0.95),
					MaxLatencyMs: maxLatencyMs(latencies),
					SampleError:  loadSampleError(&firstError),
				})
			}

			seq := sequence.Add(1)
			wg.Add(1)

			go func(requestID uint64) {
				defer wg.Done()
				defer func() { <-sem }()

				body, err := json.Marshal(map[string]any{
					"agent_id": fmt.Sprintf("phase2-agent-%d", requestID%uint64(agents)),
					"text":     fmt.Sprintf("phase2 load probe %d", requestID),
				})
				if err != nil {
					failureCount.Add(1)
					storeSampleError(&firstError, fmt.Sprintf("marshal request: %v", err))
					return
				}

				req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, targetURL, bytes.NewReader(body))
				if err != nil {
					failureCount.Add(1)
					storeSampleError(&firstError, fmt.Sprintf("build request: %v", err))
					return
				}
				req.Header.Set("Content-Type", "application/json")

				start := time.Now()
				resp, err := client.Do(req)
				latency := time.Since(start)

				latencyMu.Lock()
				latencies = append(latencies, latency)
				latencyMu.Unlock()
				totalCount.Add(1)

				if err != nil {
					failureCount.Add(1)
					storeSampleError(&firstError, fmt.Sprintf("request failed: %v", err))
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					payload, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
					failureCount.Add(1)
					storeSampleError(&firstError, fmt.Sprintf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload))))
					return
				}

				_, _ = io.Copy(io.Discard, resp.Body)
				successCount.Add(1)
			}(seq)
		}
	}
}

func validateConfig(duration, requestTimeout time.Duration, rps, agents, maxInflight int) error {
	switch {
	case duration <= 0:
		return fmt.Errorf("duration must be greater than zero")
	case requestTimeout <= 0:
		return fmt.Errorf("timeout must be greater than zero")
	case rps <= 0:
		return fmt.Errorf("rps must be greater than zero")
	case agents <= 0:
		return fmt.Errorf("agents must be greater than zero")
	case maxInflight <= 0:
		return fmt.Errorf("max-inflight must be greater than zero")
	default:
		return nil
	}
}

func reportAndExit(result summary) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "encode summary: %v\n", err)
		os.Exit(1)
	}

	if result.Total == 0 || result.Failure > 0 {
		os.Exit(1)
	}
	os.Exit(0)
}

func averageLatencyMs(latencies []time.Duration) float64 {
	if len(latencies) == 0 {
		return 0
	}
	var total time.Duration
	for _, latency := range latencies {
		total += latency
	}
	return float64(total.Milliseconds()) + float64(total%time.Millisecond)/float64(time.Millisecond)
}

func percentileLatencyMs(latencies []time.Duration, percentile float64) float64 {
	if len(latencies) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), latencies...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})
	index := int(math.Ceil(float64(len(sorted))*percentile)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return durationToMilliseconds(sorted[index])
}

func maxLatencyMs(latencies []time.Duration) float64 {
	if len(latencies) == 0 {
		return 0
	}
	var maxValue time.Duration
	for _, latency := range latencies {
		if latency > maxValue {
			maxValue = latency
		}
	}
	return durationToMilliseconds(maxValue)
}

func durationToMilliseconds(value time.Duration) float64 {
	return float64(value.Milliseconds()) + float64(value%time.Millisecond)/float64(time.Millisecond)
}

func storeSampleError(slot *atomic.Value, message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	if slot.Load() != nil {
		return
	}
	slot.Store(message)
}

func loadSampleError(slot *atomic.Value) string {
	value := slot.Load()
	if value == nil {
		return ""
	}
	message, _ := value.(string)
	return message
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
