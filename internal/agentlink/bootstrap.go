package agentlink

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Bootstrap binds the upgrade transport to the enrolled identity and a fresh client nonce.
type Bootstrap struct {
	ServerID   int64  `json:"server_id"`
	AgentID    string `json:"agent_id"`
	Address    string `json:"address"`
	CertSHA256 string `json:"cert_sha256"`
	Nonce      string `json:"nonce"`
	Signature  string `json:"signature,omitempty"`
}

func (b Bootstrap) Sign(secret string) string {
	b.Signature = ""
	data, _ := json.Marshal(b)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("agentlink_bootstrap_v1\x00"))
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}
func (b Bootstrap) Verify(secret string) bool {
	expected, _ := hex.DecodeString(b.Sign(secret))
	actual, err := hex.DecodeString(b.Signature)
	return err == nil && hmac.Equal(expected, actual)
}
