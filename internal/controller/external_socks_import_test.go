package controller

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/OboardProject/oboard/internal/model"
)

func TestParseExternalOutboundImportSocksBase64ShareLinks(t *testing.T) {
	whole := base64.RawURLEncoding.EncodeToString([]byte("carol:example-password@203.0.113.7:1080"))
	userinfo := base64.StdEncoding.EncodeToString([]byte("bob:p@ss:w"))
	content := "socks://" + whole + "?udp=1&method=auto&dialer-proxy=%F0%9F%87%AF%F0%9F%87%B5%20Fdr%EF%BD%9CCogent\n" +
		"socks5://" + userinfo + "@[2001:db8::1]:1081#Edge\n" +
		"socks5://alice:secret@socks.example.com:1082"
	items, err := parseExternalOutboundImport(content)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		host           string
		port           int
		user, password string
	}{
		{"203.0.113.7", 1080, "carol", "example-password"},
		{"2001:db8::1", 1081, "bob", "p@ss:w"},
		{"socks.example.com", 1082, "alice", "secret"},
	}
	if len(items) != len(want) {
		t.Fatalf("got %d items", len(items))
	}
	for i, w := range want {
		item := items[i]
		var cfg map[string]any
		if err := json.Unmarshal([]byte(item.ConfigJSON), &cfg); err != nil {
			t.Fatal(err)
		}
		if item.Protocol != model.ProtocolSocks || item.TargetAddress != w.host || item.TargetPort != w.port || cfg["username"] != w.user || cfg["password"] != w.password {
			t.Fatalf("item %d: %#v %#v", i, item, cfg)
		}
		if _, ok := cfg["detour"]; ok {
			t.Fatalf("item %d must not carry dialer-proxy as detour: %#v", i, cfg)
		}
	}
	if items[1].Name != "Edge" {
		t.Fatalf("name = %q", items[1].Name)
	}
}
