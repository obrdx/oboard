package controller

import (
	"time"

	"github.com/OboardProject/oboard/internal/store"
)

// proxyRateStaleAfter matches the store rule that reads an old proxy rate as
// zero. The Agent sends no traffic report while a server carries no proxy
// traffic, so without this sweep a live view would keep the last rate.
const proxyRateStaleAfter = 90 * time.Second

func (s *Server) publishProxyTraffic(proxy *store.ServerProxyTraffic, at time.Time) {
	if proxy == nil || s.realtime == nil {
		return
	}
	s.proxyRateLive.Store(proxy.ServerID, at)
	s.realtime.publishServerPatch(realtimeServerPatch{ServerID: proxy.ServerID, Fields: map[string]any{
		"proxy_traffic_upload_bytes":   proxy.UploadBytes,
		"proxy_traffic_download_bytes": proxy.DownloadBytes,
		"proxy_upload_bps":             proxy.UploadBPS,
		"proxy_download_bps":           proxy.DownloadBPS,
	}})
}

// expireProxyRates publishes a zero proxy rate for servers whose last accepted
// traffic report is older than proxyRateStaleAfter.
func (s *Server) expireProxyRates(now time.Time) {
	if s.realtime == nil {
		return
	}
	s.proxyRateLive.Range(func(key, value any) bool {
		at, _ := value.(time.Time)
		if now.Sub(at) <= proxyRateStaleAfter {
			return true
		}
		s.proxyRateLive.Delete(key)
		s.realtime.publishServerPatch(realtimeServerPatch{ServerID: key.(int64), Fields: map[string]any{"proxy_upload_bps": 0, "proxy_download_bps": 0}})
		return true
	})
}
