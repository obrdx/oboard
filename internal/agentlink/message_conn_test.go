package agentlink

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"
)

func TestInteractiveMessageConnRoundTrip(t *testing.T) {
	addr, pin := startTestServer(t, func(session *Session) {
		auth, err := session.ReadAuth()
		if err != nil {
			return
		}
		if auth.Purpose != "interactive" || auth.SessionID != "terminal-1" || auth.Proof != "proof" {
			session.RejectAuth("invalid proof")
			return
		}
		if session.AcceptAuth(nil) != nil {
			return
		}
		conn := &MessageConn{Session: session}
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.WriteMessage(kind, data)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	session, err := Dial(ctx, ClientConfig{Address: addr, CertSHA256: pin, Purpose: "interactive", SessionID: "terminal-1", Proof: "proof", AgentID: "a", Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	conn := &MessageConn{Session: session}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	data := []byte{0, 1, 2, 255, '\n'}
	if err := conn.WriteMessage(2, data); err != nil {
		t.Fatal(err)
	}
	kind, received, err := conn.ReadMessage()
	if err != nil || kind != 2 || !bytes.Equal(data, received) {
		t.Fatalf("interactive stream changed bytes: %v %d %v", err, kind, received)
	}
	if err := conn.WriteMessage(2, make([]byte, InteractiveMaxMessage+1)); err != ErrMessageTooLarge {
		t.Fatal("oversized terminal data accepted")
	}
}

func TestApplicationFramesSupportSharedMessageLimit(t *testing.T) {
	for _, kind := range []byte{frameTypeMessage, frameTypeRequest, frameTypeResponse} {
		var buf bytes.Buffer
		data := bytes.Repeat([]byte("x"), maxFramePayload+1)
		if err := writeFrame(&buf, frame{frameType: kind, payload: data}); err != nil {
			t.Fatal(err)
		}
		f, err := readFrame(&buf)
		if err != nil || len(f.payload) != len(data) {
			t.Fatalf("application frame truncated: %v", err)
		}
	}
	header := make([]byte, frameHeaderLen)
	header[0] = frameTypeMessage
	binary.BigEndian.PutUint32(header[2:], MaxMessageBytes+1)
	if _, err := readFrame(bytes.NewReader(header)); err != ErrFrameTooLarge {
		t.Fatal("oversize application frame accepted")
	}
	binary.BigEndian.PutUint32(header[2:], InteractiveMaxMessage*2+1)
	if _, err := readFrameLimited(bytes.NewReader(header), InteractiveMaxMessage*2); err != ErrFrameTooLarge {
		t.Fatal("terminal frame allocated before size check")
	}
}
