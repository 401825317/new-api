package helper

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"

	"github.com/gin-gonic/gin"
)

const (
	InitialScannerBufferSize    = 64 << 10  // 64KB (64*1024)
	DefaultMaxScannerBufferSize = 128 << 20 // 64MB (64*1024*1024) default SSE buffer size
	DefaultPingInterval         = 10 * time.Second
)

const (
	debugTimingHeader       = "X-Debug-Timing"
	newAPIDebugTimingHeader = "X-NewAPI-Debug-Timing"
)

type streamTimingDebug struct {
	enabled bool
	start   time.Time
	ctx     *gin.Context
	info    *relaycommon.RelayInfo

	mu                     sync.Mutex
	firstDataMs            int64
	firstContentLikeDataMs int64
	firstHandlerDoneMs     int64
}

type streamDataChunk struct {
	data     string
	received int
}

func getScannerBufferSize() int {
	if constant.StreamScannerMaxBufferMB > 0 {
		return constant.StreamScannerMaxBufferMB << 20
	}
	return DefaultMaxScannerBufferSize
}

func NewStreamScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, InitialScannerBufferSize), getScannerBufferSize())
	return scanner
}

func newStreamTimingDebug(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) *streamTimingDebug {
	debug := &streamTimingDebug{
		ctx:  c,
		info: info,
	}
	if c == nil || c.Request == nil || !streamTimingDebugEnabled(c.Request.Header) {
		return debug
	}
	debug.enabled = true
	debug.start = info.StartTime
	if debug.start.IsZero() {
		debug.start = time.Now()
	}

	logger.LogInfo(c, fmt.Sprintf(
		"stream timing start channel_id=%d channel_type=%d model=%s upstream_model=%s path=%s upstream_status=%d upstream_content_type=%s upstream_request_id=%s cf_ray=%s",
		streamTimingChannelID(info),
		streamTimingChannelType(info),
		safeTimingLogValue(info.OriginModelName),
		safeTimingLogValue(streamTimingUpstreamModel(info)),
		safeTimingLogValue(info.RequestURLPath),
		streamTimingStatusCode(resp),
		safeTimingLogValue(streamTimingHeaderValue(resp, "Content-Type")),
		safeTimingLogValue(streamTimingHeaderValue(resp, common.UpstreamRequestIdKey)),
		safeTimingLogValue(streamTimingHeaderValue(resp, "Cf-Ray")),
	))

	return debug
}

func streamTimingChannelID(info *relaycommon.RelayInfo) int {
	if info == nil || info.ChannelMeta == nil {
		return 0
	}
	return info.ChannelId
}

func streamTimingChannelType(info *relaycommon.RelayInfo) int {
	if info == nil || info.ChannelMeta == nil {
		return 0
	}
	return info.ChannelType
}

func streamTimingUpstreamModel(info *relaycommon.RelayInfo) string {
	if info == nil || info.ChannelMeta == nil {
		return ""
	}
	return info.UpstreamModelName
}

func streamTimingDebugEnabled(header http.Header) bool {
	return isTruthyDebugTimingValue(header.Get(debugTimingHeader)) ||
		isTruthyDebugTimingValue(header.Get(newAPIDebugTimingHeader))
}

func DebugTimingEnabled(header http.Header) bool {
	return streamTimingDebugEnabled(header)
}

func isTruthyDebugTimingValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func streamTimingStatusCode(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

func streamTimingHeaderValue(resp *http.Response, key string) string {
	if resp == nil {
		return ""
	}
	return resp.Header.Get(key)
}

func safeTimingLogValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return strconv.Quote(value)
}

func (d *streamTimingDebug) elapsedMs() int64 {
	return time.Since(d.start).Milliseconds()
}

func (d *streamTimingDebug) markFirstData(data string, received int) {
	if d == nil || !d.enabled {
		return
	}

	contentLike := isStreamTimingContentLikeData(data)
	d.mu.Lock()
	if d.firstDataMs == 0 {
		d.firstDataMs = d.elapsedMs()
		logger.LogInfo(d.ctx, fmt.Sprintf(
			"stream timing first_data_ms=%d content_like=%t received=%d",
			d.firstDataMs,
			contentLike,
			received,
		))
	}
	if contentLike && d.firstContentLikeDataMs == 0 {
		d.firstContentLikeDataMs = d.elapsedMs()
		logger.LogInfo(d.ctx, fmt.Sprintf(
			"stream timing first_content_like_data_ms=%d received=%d",
			d.firstContentLikeDataMs,
			received,
		))
	}
	d.mu.Unlock()
}

func (d *streamTimingDebug) markFirstHandlerDone(received int) {
	if d == nil || !d.enabled {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.firstHandlerDoneMs != 0 {
		return
	}
	d.firstHandlerDoneMs = d.elapsedMs()
	logger.LogInfo(d.ctx, fmt.Sprintf(
		"stream timing first_handler_done_ms=%d received=%d",
		d.firstHandlerDoneMs,
		received,
	))
}

func (d *streamTimingDebug) finish() {
	if d == nil || !d.enabled {
		return
	}

	d.mu.Lock()
	firstDataMs := d.firstDataMs
	firstContentLikeDataMs := d.firstContentLikeDataMs
	firstHandlerDoneMs := d.firstHandlerDoneMs
	d.mu.Unlock()

	endReason := relaycommon.StreamEndReasonNone
	errorCount := 0
	if d.info != nil && d.info.StreamStatus != nil {
		endReason = d.info.StreamStatus.EndReason
		errorCount = d.info.StreamStatus.TotalErrorCount()
	}
	received := 0
	if d.info != nil {
		received = d.info.ReceivedResponseCount
	}

	logger.LogInfo(d.ctx, fmt.Sprintf(
		"stream timing done total_ms=%d first_data_ms=%d first_content_like_data_ms=%d first_handler_done_ms=%d received=%d end_reason=%s soft_errors=%d",
		d.elapsedMs(),
		firstDataMs,
		firstContentLikeDataMs,
		firstHandlerDoneMs,
		received,
		endReason,
		errorCount,
	))
}

func isStreamTimingContentLikeData(data string) bool {
	data = strings.TrimSpace(data)
	if data == "" || strings.HasPrefix(data, "[DONE]") {
		return false
	}

	contentMarkers := []string{
		`"content":`,
		`"reasoning_content":`,
		`"tool_calls":`,
		`"function_call":`,
		`"function_call_arguments":`,
		`"response.output_text.delta"`,
		`"response.reasoning_summary_text.delta"`,
		`"response.function_call_arguments.delta"`,
		`"content_block_delta"`,
		`"text_delta"`,
	}
	for _, marker := range contentMarkers {
		if strings.Contains(data, marker) {
			return true
		}
	}
	return false
}

func StreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(data string, sr *StreamResult)) {

	if resp == nil || dataHandler == nil {
		return
	}

	// 无条件新建 StreamStatus
	info.StreamStatus = relaycommon.NewStreamStatus()
	timingDebug := newStreamTimingDebug(c, resp, info)
	defer timingDebug.finish()

	// 确保响应体总是被关闭
	defer func() {
		if resp.Body != nil {
			resp.Body.Close()
		}
	}()

	streamingTimeout := time.Duration(constant.StreamingTimeout) * time.Second

	var (
		stopChan   = make(chan bool, 3) // 增加缓冲区避免阻塞
		scanner    = NewStreamScanner(resp.Body)
		ticker     = time.NewTicker(streamingTimeout)
		pingTicker *time.Ticker
		writeMutex sync.Mutex     // Mutex to protect concurrent writes
		wg         sync.WaitGroup // 用于等待所有 goroutine 退出
	)

	generalSettings := operation_setting.GetGeneralSetting()
	pingEnabled := generalSettings.PingIntervalEnabled && !info.DisablePing
	pingInterval := time.Duration(generalSettings.PingIntervalSeconds) * time.Second
	if pingInterval <= 0 {
		pingInterval = DefaultPingInterval
	}

	if pingEnabled {
		pingTicker = time.NewTicker(pingInterval)
	}

	logger.LogDebug(c, "relay timeout seconds: %d", common.RelayTimeout)
	logger.LogDebug(c, "relay max idle conns: %d", common.RelayMaxIdleConns)
	logger.LogDebug(c, "relay max idle conns per host: %d", common.RelayMaxIdleConnsPerHost)
	logger.LogDebug(c, "streaming timeout seconds: %d", int64(streamingTimeout.Seconds()))
	logger.LogDebug(c, "ping interval seconds: %d", int64(pingInterval.Seconds()))

	// 改进资源清理，确保所有 goroutine 正确退出
	defer func() {
		// 通知所有 goroutine 停止
		common.SafeSendBool(stopChan, true)

		ticker.Stop()
		if pingTicker != nil {
			pingTicker.Stop()
		}

		// 等待所有 goroutine 退出，最多等待5秒
		done := make(chan struct{})
		gopool.Go(func() {
			wg.Wait()
			close(done)
		})

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			logger.LogError(c, "timeout waiting for goroutines to exit")
		}

		close(stopChan)
	}()

	scanner.Split(bufio.ScanLines)
	SetEventStreamHeaders(c)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctx = context.WithValue(ctx, "stop_chan", stopChan)

	// Handle ping data sending with improved error handling
	if pingEnabled && pingTicker != nil {
		wg.Add(1)
		gopool.Go(func() {
			defer func() {
				wg.Done()
				if r := recover(); r != nil {
					logger.LogError(c, fmt.Sprintf("ping goroutine panic: %v", r))
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("ping panic: %v", r))
					common.SafeSendBool(stopChan, true)
				}
				logger.LogDebug(c, "ping goroutine exited")
			}()

			// 添加超时保护，防止 goroutine 无限运行
			maxPingDuration := 30 * time.Minute // 最大 ping 持续时间
			pingTimeout := time.NewTimer(maxPingDuration)
			defer pingTimeout.Stop()

			for {
				select {
				case <-pingTicker.C:
					// 使用超时机制防止写操作阻塞
					done := make(chan error, 1)
					gopool.Go(func() {
						writeMutex.Lock()
						defer writeMutex.Unlock()
						done <- PingData(c)
					})

					select {
					case err := <-done:
						if err != nil {
							logger.LogError(c, "ping data error: "+err.Error())
							info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPingFail, err)
							return
						}
						logger.LogDebug(c, "ping data sent")
					case <-time.After(10 * time.Second):
						logger.LogError(c, "ping data send timeout")
						info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPingFail, fmt.Errorf("ping send timeout"))
						return
					case <-ctx.Done():
						return
					case <-stopChan:
						return
					}
				case <-ctx.Done():
					return
				case <-stopChan:
					return
				case <-c.Request.Context().Done():
					// 监听客户端断开连接
					return
				case <-pingTimeout.C:
					logger.LogError(c, "ping goroutine max duration reached")
					return
				}
			}
		})
	}

	dataChan := make(chan streamDataChunk, 10)

	wg.Add(1)
	gopool.Go(func() {
		defer func() {
			wg.Done()
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("data handler goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("handler panic: %v", r))
			}
			common.SafeSendBool(stopChan, true)
		}()
		sr := newStreamResult(info.StreamStatus)
		for chunk := range dataChan {
			sr.reset()
			writeMutex.Lock()
			dataHandler(chunk.data, sr)
			writeMutex.Unlock()
			timingDebug.markFirstHandlerDone(chunk.received)
			if sr.IsStopped() {
				return
			}
		}
	})

	// Scanner goroutine with improved error handling
	wg.Add(1)
	common.RelayCtxGo(ctx, func() {
		defer func() {
			close(dataChan)
			wg.Done()
			if r := recover(); r != nil {
				logger.LogError(c, fmt.Sprintf("scanner goroutine panic: %v", r))
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("scanner panic: %v", r))
			}
			common.SafeSendBool(stopChan, true)
			logger.LogDebug(c, "scanner goroutine exited")
		}()

		for scanner.Scan() {
			// 检查是否需要停止
			select {
			case <-stopChan:
				return
			case <-ctx.Done():
				return
			case <-c.Request.Context().Done():
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
				return
			default:
			}

			ticker.Reset(streamingTimeout)
			data := scanner.Text()
			logger.LogDebug(c, "stream scanner data: %s", data)

			if len(data) < 6 {
				continue
			}
			if data[:5] != "data:" && data[:6] != "[DONE]" {
				continue
			}
			data = data[5:]
			data = strings.TrimSpace(data)
			if data == "" {
				continue
			}
			if !strings.HasPrefix(data, "[DONE]") {
				info.SetFirstResponseTime()
				info.ReceivedResponseCount++
				timingDebug.markFirstData(data, info.ReceivedResponseCount)

				select {
				case dataChan <- streamDataChunk{data: data, received: info.ReceivedResponseCount}:
				case <-ctx.Done():
					return
				case <-stopChan:
					return
				}
			} else {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				logger.LogDebug(c, "received [DONE], stopping scanner")
				return
			}
		}

		if err := scanner.Err(); err != nil {
			if err != io.EOF {
				logger.LogError(c, "scanner error: "+err.Error())
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
			}
		}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, nil)
	})

	// 主循环等待完成或超时
	select {
	case <-ticker.C:
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
	case <-stopChan:
		// EndReason already set by the goroutine that triggered stopChan
	case <-c.Request.Context().Done():
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
	}

	if info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors() {
		logger.LogInfo(c, fmt.Sprintf("stream ended: %s", info.StreamStatus.Summary()))
	} else {
		logger.LogError(c, fmt.Sprintf("stream ended: %s, received=%d", info.StreamStatus.Summary(), info.ReceivedResponseCount))
	}
}
