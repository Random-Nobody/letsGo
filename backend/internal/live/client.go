// room/client.go
package live

import (
	"context"
	"encoding/json"
	"letsgo/internal/config"
	"letsgo/internal/token"
	"log/slog"
	"time"

	"github.com/coder/websocket"
)

type client struct {
	cfg    *config.WS
	ID     string
	Hub    *hub
	Conn   *websocket.Conn
	Send   chan []byte
	Room   string
	User   *token.UserPayload
	ctx    context.Context
	cancel context.CancelFunc
}

func newClient(hub *hub, conn *websocket.Conn, user *token.UserPayload, cfg *config.WS) *client {
	ctx, cancel := context.WithCancel(context.Background())
	return &client{
		cfg:    cfg,
		ID:     user.Username,
		Hub:    hub,
		Conn:   conn,
		Send:   make(chan []byte, cfg.SendBuffer),
		User:   user,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (c *client) start() {
	go c.writePump()
	go c.readPump()
	go c.pingPump()
}

func (c *client) readPump() {
	defer func() {
		c.Hub.unregister <- c
	}()

	for {
		msgType, msgRaw, err := c.Conn.Read(c.ctx)
		if err != nil {
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure &&
				websocket.CloseStatus(err) != websocket.StatusGoingAway {
				slog.Error("readPump: WebSocket read error", "error", err, "client", c.ID)
			}
			break
		}
		if msgType != websocket.MessageText {
			slog.Error("readPump: unhandled message type", "msgType", msgType.String())
			c.Send <- createMsg(msgError, "message", "Invalid message type")
			continue
		}
		if len(msgRaw) > int(c.cfg.MaxMsgSize) {
			c.Send <- createMsg(msgError, "message", "Message too large.")
			continue
		}
		var msg roomMsg
		if err := json.Unmarshal(msgRaw, &msg); err != nil {
			c.Send <- createMsg(msgError, "message", "Invalid message format: "+err.Error())
			continue
		}

		msg.Sender = c.ID

		switch msg.Type {
		case msgChat, msgVidSignal, msgGameState:
			c.Hub.messages <- &msg
		case msgJoinRoom:
			if payloadMap, ok := msg.Payload.(map[string]any); ok {
				if roomID, ok := payloadMap["roomName"].(string); ok && roomID != "" {
					c.Hub.joinRoom <- &crPair{Client: c, RoomName: roomID}
				} else {
					c.Send <- createMsg(msgError, "message", "invalid format: missing roomName")
				}
			} else {
				c.Send <- createMsg(msgError, "message", "invalid format for join room")
			}
		case msgLeaveRoom:
			c.Hub.leaveRoom <- c
		case msgGetClients:
			if c.Room != "" {
				if room, ok := c.Hub.rooms[c.Room]; ok {
					c.Send <- room.getClientList()
				}
			}
		default:
			slog.Warn("readPump: Unknown message type received", "type", msg.Type, "client", c.ID)
			c.Send <- createMsg(msgError, "message", "Unknown message type: "+msg.Type)
		}
	}
}

func (c *client) writePump() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case message, ok := <-c.Send:
			if !ok {
				c.cancel()
				return
			}
			writeCtx, cancelWrite := context.WithTimeout(c.ctx, c.cfg.WriteTimeout)
			err := c.Conn.Write(writeCtx, websocket.MessageText, message)
			cancelWrite()
			if err != nil {
				slog.Error("writePump: WebSocket write error", "error", err, "client", c.ID)
				c.cancel()
				return
			}
		}
	}
}

func (c *client) pingPump() {
	ticker := time.NewTicker(c.cfg.PongTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancelPing := context.WithTimeout(c.ctx, c.cfg.PongTimeout)
			err := c.Conn.Ping(pingCtx)
			cancelPing()
			if err != nil {
				slog.Error("Client ping failed", "error", err, "client", c.ID)
				c.cancel()
				return
			}
		}
	}
}
