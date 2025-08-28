// gen_logs.go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"
)

type logEntry struct {
	Timestamp      string   `json:"timestamp"`
	UserID         string   `json:"user_id"`
	ResponseTimeMs *float64 `json:"response_time_ms"`
	HTTPStatus     int      `json:"http_status"`
}

func pickWeightedStatus(r *rand.Rand) int {
	// 定义状态码及权重（总和不必精确为1）
	statuses := []int{200, 404, 500, 302, 503}
	weights := []float64{0.85, 0.08, 0.02, 0.03, 0.02}
	// 计算累积权重
	var total float64
	for _, w := range weights {
		total += w
	}
	p := r.Float64() * total
	acc := 0.0
	for i, w := range weights {
		acc += w
		if p <= acc {
			return statuses[i]
		}
	}
	return statuses[len(statuses)-1]
}

func randResponseMs(r *rand.Rand) float64 {
	// 大多数请求在 20-400ms，极少数出现长尾
	base := float64(r.Intn(380) + 20) // 20-399
	if r.Float64() < 0.01 {           // 1% 概率出现长尾
		base += float64(r.Intn(3000))
	}
	return base
}

func randUserID(r *rand.Rand, maxUsers int) string {
	id := r.Intn(maxUsers) + 1
	return "u" + fmt.Sprintf("%03d", id)
}

func main() {
	var (
		lines    = flag.Int("lines", 1000, "要生成的日志行数")
		outfile  = flag.String("out", "access.log", "输出日志文件路径")
		startDay = flag.String("start", "", "起始日期 (YYYY-MM-DD)，默认今天（UTC）")
		days     = flag.Int("days", 1, "跨越多少天生成时间戳（默认1天）")
		users    = flag.Int("users", 100, "模拟的不同用户数量 (user_id 从 u001 开始)")
		seed     = flag.Int64("seed", 0, "随机种子（默认：time.Now().UnixNano()）")
	)
	flag.Parse()

	// 解析或设置随机种子
	var s int64
	if *seed == 0 {
		s = time.Now().UnixNano()
	} else {
		s = *seed
	}
	r := rand.New(rand.NewSource(s))

	// 计算时间范围
	var start time.Time
	if *startDay == "" {
		// 使用当前 UTC 日期的 00:00:00
		now := time.Now().UTC()
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	} else {
		t, err := time.Parse("2006-01-02", *startDay)
		if err != nil {
			fmt.Fprintf(os.Stderr, "无法解析 start 参数: %v\n", err)
			os.Exit(1)
		}
		start = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	end := start.Add(time.Duration(*days) * 24 * time.Hour)

	// 打开输出文件
	f, err := os.Create(*outfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法创建输出文件: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	// 逐行生成 JSON
	for i := 0; i < *lines; i++ {
		// 随机时间戳：在 [start, end) 之间均匀分布
		delta := r.Int63n(end.Unix() - start.Unix())
		ts := time.Unix(start.Unix()+delta, 0).UTC().Format(time.RFC3339)

		status := pickWeightedStatus(r)

		// 少数情况下模拟缺失 response_time_ms（比如 2%）
		var respPtr *float64
		if r.Float64() < 0.02 {
			respPtr = nil
		} else {
			val := randResponseMs(r)
			respPtr = &val
		}

		entry := logEntry{
			Timestamp:      ts,
			UserID:         randUserID(r, *users),
			ResponseTimeMs: respPtr,
			HTTPStatus:     status,
		}

		js, err := json.Marshal(entry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "json.Marshal 错误: %v\n", err)
			continue
		}
		// 每行一个 JSON 并换行
		f.Write(js)
		f.Write([]byte("\n"))
	}

	fmt.Fprintf(os.Stderr, "生成完成：%s (lines=%d, seed=%d)\n", *outfile, *lines, s)
}
