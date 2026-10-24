package agentlink

import (
	"encoding/json"
	"errors"
	"time"
)

// MessageConn carries a bounded interactive stream on a separately authenticated session.
// It owns the reader; Session.Run must not run on the same session.
type MessageConn struct{ Session *Session }

type streamMessage struct {
	Kind int    `json:"kind"`
	Data []byte `json:"data"`
}

const (
	InteractiveMaxMessage = 64 << 10
	TextMessage           = 1
	BinaryMessage         = 2
)

func (c *MessageConn) ReadMessage() (int, []byte, error) {
	f, err := readFrameLimited(c.Session.conn, InteractiveMaxMessage*2)
	if err != nil {
		return 0, nil, err
	}
	if f.frameType != frameTypeMessage {
		return 0, nil, errors.New("invalid interactive frame")
	}
	payload := f.payload
	if f.flags&flagPad != 0 {
		payload, err = stripPadding(payload)
		if err != nil {
			return 0, nil, err
		}
	}
	if len(payload) > InteractiveMaxMessage*2 {
		return 0, nil, ErrMessageTooLarge
	}
	var msg streamMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return 0, nil, err
	}
	if (msg.Kind != 1 && msg.Kind != 2) || len(msg.Data) > InteractiveMaxMessage {
		return 0, nil, ErrMessageTooLarge
	}
	return msg.Kind, msg.Data, nil
}
func (c *MessageConn) WriteMessage(kind int, data []byte) error {
	if (kind != 1 && kind != 2) || len(data) > InteractiveMaxMessage {
		return ErrMessageTooLarge
	}
	return c.Session.WriteMessage(streamMessage{Kind: kind, Data: data})
}
func (c *MessageConn) WriteJSON(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.WriteMessage(1, data)
}
func (c *MessageConn) SetReadDeadline(deadline time.Time) error {
	return c.Session.conn.SetReadDeadline(deadline)
}
func (c *MessageConn) Close() error { return c.Session.Close() }
