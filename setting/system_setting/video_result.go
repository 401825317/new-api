package system_setting

import "strings"

const (
	VideoResultURLModeProxy      = "proxy"
	VideoResultURLModeDownstream = "downstream"
)

func NormalizeVideoResultURLMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case VideoResultURLModeDownstream, "direct", "upstream", "origin":
		return VideoResultURLModeDownstream
	case VideoResultURLModeProxy:
		return VideoResultURLModeProxy
	default:
		return VideoResultURLModeProxy
	}
}

func UseDownstreamVideoResultURL() bool {
	return NormalizeVideoResultURLMode(VideoResultURLMode) == VideoResultURLModeDownstream
}
