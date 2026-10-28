package core

import (
	"encoding/json"
	"github.com/OboardProject/oboard/internal/model"
	"testing"
)

func TestSnellV6ProfilePreservesIndependentCredentials(t *testing.T) {
	cfg, _ := json.Marshal(map[string]any{"version": 6, "listener_mode": "shared_port", "psk": "unused-profile-psk", "mode": "default"})
	in := model.Inbound{ID: 1, Protocol: model.ProtocolSnell, ListenIP: "0.0.0.0", Port: 7177, ConfigJSON: string(cfg)}
	users := []model.User{
		{ID: 1, Username: "alice", AuthorizationKey: "key-a", ProxyPassword: "alice-independent-psk"},
		{ID: 2, Username: "bob", AuthorizationKey: "key-b", ProxyPassword: "bob-independent-psk"},
	}
	runtime, err := (snellAdapter{}).Inbound(in, users)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runtime["psk"]; ok {
		t.Fatal("global PSK leaked into shared listener")
	}
	entries := runtime["users"].([]map[string]any)
	for i, user := range users {
		node, ok, err := SnellSubscriptionNode(nil, user, in, model.Server{EntryAddress: "example.com"}, 0)
		if err != nil || !ok {
			t.Fatalf("subscription: ok=%v err=%v", ok, err)
		}
		if entries[i]["psk"] != user.ProxyPassword || node["psk"] != user.ProxyPassword {
			t.Fatal("profile replaced identity credential")
		}
	}
}
