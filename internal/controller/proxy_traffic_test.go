package controller

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/OboardProject/oboard/internal/store"
)

func TestExpireProxyRatesZeroesOnlyStaleServers(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "oboard.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := newTestServer(db, "test-secret", "")
	now := time.Now()
	s.publishProxyTraffic(&store.ServerProxyTraffic{ServerID: 1, UploadBPS: 10, DownloadBPS: 20}, now.Add(-2*time.Minute))
	s.publishProxyTraffic(&store.ServerProxyTraffic{ServerID: 2, UploadBPS: 30, DownloadBPS: 40}, now.Add(-10*time.Second))

	s.expireProxyRates(now)

	if _, ok := s.proxyRateLive.Load(int64(1)); ok {
		t.Fatal("stale server 1 still tracked")
	}
	if _, ok := s.proxyRateLive.Load(int64(2)); !ok {
		t.Fatal("fresh server 2 was expired")
	}
	s.realtime.mu.Lock()
	stale := s.realtime.serverPatches[1]
	fresh := s.realtime.serverPatches[2]
	s.realtime.mu.Unlock()
	if stale["proxy_download_bps"] != 0 || stale["proxy_upload_bps"] != 0 {
		t.Fatalf("stale patch = %#v", stale)
	}
	if fresh["proxy_download_bps"] != uint64(40) {
		t.Fatalf("fresh patch = %#v", fresh)
	}
}
