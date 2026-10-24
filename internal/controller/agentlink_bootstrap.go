package controller

import (
	"encoding/hex"
	"net/http"
	"time"

	"github.com/OboardProject/oboard/internal/agentlink"
)

// agentLinkBootstrap is retained only for automatic upgrades from HTTP/WSS Agents.
func (s *Server) agentLinkBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		method(w)
		return
	}
	server, ok := s.authAgent(w, r)
	if !ok {
		return
	}
	if !s.allowAgentRate(w, "agent-transport:"+server.AgentID, 10, time.Minute) {
		return
	}
	nonce := r.Header.Get("X-Oboard-Transport-Nonce")
	decoded, err := hex.DecodeString(nonce)
	if err != nil || len(decoded) != 32 {
		failCode(w, "invalid_nonce", "invalid transport nonce", http.StatusBadRequest)
		return
	}
	addr, pin, active := s.StealthTransportInfo()
	if !active {
		failCode(w, "agentlink_unavailable", "请先填写并启用 AgentLink 专用地址", http.StatusPreconditionFailed)
		return
	}
	response := agentlink.Bootstrap{ServerID: server.ID, AgentID: server.AgentID, Address: addr, CertSHA256: pin, Nonce: nonce}
	response.Signature = response.Sign(server.AgentTokenHash)
	w.Header().Set("Cache-Control", "no-store")
	write(w, http.StatusOK, response)
}
