package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/urfave/cli/v2"
)

// LogEntry 对应日志行的字段
type LogEntry struct {
	Timestamp      string   `json:"timestamp"`
	ResponseTimeMs *float64 `json:"response_time_ms"`
	HTTPStatus     int      `json:"http_status"`
}

// Output 最终输出结构
type Output struct {
	TotalRequests         int            `json:"total_requests"`
	AverageResponseTimeMs float64        `json:"average_response_time_ms"`
	StatusCodeCounts      map[string]int `json:"status_code_counts"`
	BusiestHour           *int           `json:"busiest_hour"`
}

// Aggregator 线程安全地聚合中间结果
type Aggregator struct {
	mu           sync.Mutex
	total        int
	sumResp      float64
	respCount    int
	statusCounts map[string]int
	hourCounts   [24]int
}

func NewAggregator() *Aggregator {
	return &Aggregator{
		statusCounts: make(map[string]int),
	}
}

func (a *Aggregator) Add(e *LogEntry, lineNo int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.total++

	if e.ResponseTimeMs != nil {
		a.sumResp += *e.ResponseTimeMs
		a.respCount++
	}

	statusKey := strconv.Itoa(e.HTTPStatus)
	a.statusCounts[statusKey]++

	if e.Timestamp != "" {
		// 解析 RFC3339 时间（兼容带 Z 或带偏移的 ISO8601）
		if t, err := time.Parse(time.RFC3339, e.Timestamp); err == nil {
			h := t.Hour()
			if h >= 0 && h < 24 {
				a.hourCounts[h]++
			}
		} else {
			// 时间解析失败写到 stderr（不影响整体）
			fmt.Fprintf(os.Stderr, "warning: cannot parse timestamp at line %d: %v\n", lineNo, err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "warning: missing timestamp at line %d\n", lineNo)
	}
}

func main() {
	app := &cli.App{
		Name:  "loganalyzer",
		Usage: "Analyze newline-delimited JSON access logs and print metrics as JSON",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "workers",
				Aliases: []string{"w"},
				Value:   4,
				Usage:   "number of concurrent worker goroutines parsing lines",
			},
		},
		Action: func(c *cli.Context) error {
			if c.NArg() < 1 {
				return fmt.Errorf("usage: %s [--workers N] /path/to/access.log", c.App.Name)
			}
			path := c.Args().Get(0)
			workers := c.Int("workers")
			if workers <= 0 {
				workers = 1
			}
			return run(path, workers)
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatalf("error: %v\n", err)
	}
}

func run(path string, workers int) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// 扩大缓冲以支持较长行
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 16*1024*1024) // 16MB 上限

	lines := make(chan []byte, 1024)
	var wg sync.WaitGroup
	agg := NewAggregator()

	// 启动 worker
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for b := range lines {
				var e LogEntry
				if err := json.Unmarshal(b, &e); err != nil {
					// 解析失败写 stderr（无法知道行号，这里不保存行号）
					fmt.Fprintf(os.Stderr, "skip invalid json (worker %d): %v\n", id, err)
					continue
				}
				// 注意：无法传入精确行号到 worker，这里不严格依赖行号
				agg.Add(&e, 0)
			}
		}(i)
	}

	// 将行送入 channel（保留行号以便警告更精确）
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		// scanner 的 buffer 会复用，必须复制数据后再发送
		cp := make([]byte, len(raw))
		copy(cp, raw)
		lines <- cp
	}
	if err := scanner.Err(); err != nil {
		close(lines)
		wg.Wait()
		return fmt.Errorf("error scanning file: %w", err)
	}

	close(lines)
	wg.Wait()

	// 计算平均响应时间
	avg := 0.0
	if agg.respCount > 0 {
		avg = agg.sumResp / float64(agg.respCount)
	}

	// 计算 busiest hour（若没有任何小时计数则为 nil）
	var busiest *int
	totalHourCounts := 0
	for _, c := range agg.hourCounts {
		totalHourCounts += c
	}
	if totalHourCounts > 0 {
		max := -1
		best := 0
		for h, c := range agg.hourCounts {
			if c > max {
				max = c
				best = h
			}
		}
		busiest = new(int)
		*busiest = best
	}

	out := Output{
		TotalRequests:         agg.total,
		AverageResponseTimeMs: avg,
		StatusCodeCounts:      agg.statusCounts,
		BusiestHour:           busiest,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("failed to encode output: %w", err)
	}

	return nil
}
