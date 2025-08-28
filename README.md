# 1. 使用示例

日志生成

编译并生成 10,000 行：

go run gen_logs.go --lines 10000 --out sample_access.log

指定起始日期并跨 3 天生成：

go run gen_logs.go --lines 50000 --out big.log --start 2025-08-25 --days 3 --users 500


固定随机种子（便于复现）：

go run gen_logs.go --lines 2000 --out test.log --seed 42

日志分析

直接运行
go run logger.go --workers 8 /path/to/big.log

或编译后运行
go build -o logger
./logger --workers 8 /path/to/big.log
<img width="1539" height="612" alt="image" src="https://github.com/user-attachments/assets/fce4e335-5b60-4b6b-a852-d024e8031ff5" />
# 2. 实现思路

逐行流式读取（streaming）：使用 bufio.Scanner 或类似的逐行读取方法，一次只把一行加载到内存，避免把整个文件读入，适合大文件。注意 Scanner 会复用底层缓冲区，若要把行数据传到 goroutine 中，必须先复制字节切片（copy）。

数据结构:

status_code_counts：使用 map[string]int统计不同 HTTP 状态码的出现次数。

理由：状态码种类非常少但不固定，map 提供 O(1) 平均时间复杂度的增量计数，语义直观，代码简洁。

hour_counts：使用固定长度的整型数组 [24]int 或 []int 长度 24 来统计每天 0–23 小时的请求量。

理由：小时范围固定且小，数组比 map 更高效（更低开销和更快访问）。

# 3. 可以优化的地方

总体复杂度与瓶颈：

时间复杂度：总体为 O(N)（N 为行数），每行做常数时间的解析与计数（忽略 json 解析常数）。

内存复杂度：当前实现只保留常量级数据（状态码 map、24 小时数组、若干标量），内存使用非常小 —— 但 JSON 解析会产生临时分配，频繁 GC 可能成为瓶颈。

具体优化：

1、减少锁竞争 — per-worker 本地聚合并合并:

每个 worker 保持自己的 map[string]int、[24]int、sumResp/respCount，解析完成后主线程只需合并这些局部结果一次。

2、IO 优化:

使用 bufio.Reader 并设置更大的缓冲区，或直接使用 mmap对超大文件做快速读取，减少系统调用次数。

或日志压缩（gzip），使用流式解压（gzip.NewReader）直接处理压缩文件，避免磁盘上额外的解压步骤。

3、降低 JSON 解析开销

使用更快的 JSON 库或手写轻量解析，减少 GC 与分配。或者使用 encoding/json.Decoder 流式解析。

