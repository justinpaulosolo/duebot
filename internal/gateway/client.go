package gateway

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const baseGatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"

type Client struct {
	Token   string
	Intents int

	// OnMessageCreate is called for every MESSAGE_CREATE event.
	// Set this before calling Run.
	OnMessageCreate func(MessageCreate)

	mu        sync.Mutex
	conn      *websocket.Conn
	sessionID string
	resumeURL string
	seq       *int

	heartbeatInterval time.Duration
	lastAckReceived   bool

	closed chan struct{}
}

func (c *Client) Run() error {
	c.closed = make(chan struct{})
	for {
		select {
		case <-c.closed:
			return nil
		default:
		}

		var err error
		if c.sessionID != "" && c.resumeURL != "" {
			err = c.connectAndResume()
		} else {
			err = c.connectAndIdentify()
		}
		if err != nil {
			log.Printf("gateway: connection ended: %v — reconnecting in 3s", err)
			time.Sleep(3 * time.Second)
			continue
		}
	}
}

func (c *Client) Close() {
	if c.closed != nil {
		select {
		case <-c.closed:
		default:
			close(c.closed)
		}
	}
	c.mu.Lock()
	if c.conn != nil {
		c.conn.Close()
	}
	c.mu.Unlock()
}

func (c *Client) connectAndIdentify() error {
	conn, _, err := websocket.DefaultDialer.Dial(baseGatewayURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	hello, err := c.readHello()
	if err != nil {
		return err
	}
	c.heartbeatInterval = time.Duration(hello.HeartbeatInterval) * time.Millisecond

	if err := c.identify(); err != nil {
		return err
	}

	return c.runSocket()
}

func (c *Client) connectAndResume() error {
	conn, _, err := websocket.DefaultDialer.Dial(c.resumeURL+"/?v=10&encoding=json", nil)
	if err != nil {
		return fmt.Errorf("dial for resume: %w", err)
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	hello, err := c.readHello()
	if err != nil {
		return err
	}
	c.heartbeatInterval = time.Duration(hello.HeartbeatInterval) * time.Millisecond

	seq := 0
	if c.seq != nil {
		seq = *c.seq
	}
	resume := resumeData{Token: c.Token, SessionID: c.sessionID, Seq: seq}
	if err := c.send(OpResume, resume); err != nil {
		return err
	}

	return c.runSocket()
}

func (c *Client) readHello() (helloData, error) {
	var p Payload
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if err := conn.ReadJSON(&p); err != nil {
		return helloData{}, fmt.Errorf("reading hello: %w", err)
	}
	if p.Op != OpHello {
		return helloData{}, fmt.Errorf("expected Hello (op 10), got op %d", p.Op)
	}
	var h helloData
	if err := json.Unmarshal(p.D, &h); err != nil {
		return helloData{}, err
	}
	return h, nil
}

func (c *Client) identify() error {
	id := identifyData{
		Token:   c.Token,
		Intents: c.Intents,
		Properties: identifyProps{
			OS:      "linux",
			Browser: "canvasbot",
			Device:  "canvasbot",
		},
	}
	return c.send(OpIdentify, id)
}

func (c *Client) runSocket() error {
	c.lastAckReceived = true
	stopHeartbeat := make(chan struct{})
	var hbWG sync.WaitGroup
	hbWG.Add(1)
	go func() {
		defer hbWG.Done()
		c.heartbeatLoop(stopHeartbeat)
	}()
	defer func() {
		close(stopHeartbeat)
		hbWG.Wait()
	}()

	for {
		var p Payload
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if err := conn.ReadJSON(&p); err != nil {
			return fmt.Errorf("read: %w", err)
		}

		if p.S != nil {
			c.mu.Lock()
			c.seq = p.S
			c.mu.Unlock()
		}

		switch p.Op {
		case OpDispatch:
			c.handleDispatch(p)
		case OpHeartbeat:
			if err := c.sendHeartbeat(); err != nil {
				return err
			}
		case OpHeartbeatACK:
			c.mu.Lock()
			c.lastAckReceived = true
			c.mu.Unlock()
		case OpReconnect:
			return fmt.Errorf("server requested reconnect")
		case OpInvalidSession:
			var resumable bool
			_ = json.Unmarshal(p.D, &resumable)
			if !resumable {
				c.mu.Lock()
				c.sessionID = ""
				c.resumeURL = ""
				c.seq = nil
				c.mu.Unlock()
			}
			return fmt.Errorf("invalid session (resumable=%v)", resumable)
		}
	}
}

func (c *Client) heartbeatLoop(stop chan struct{}) {
	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			c.mu.Lock()
			lastAck := c.lastAckReceived
			c.mu.Unlock()
			if !lastAck {
				// discord didnt ack force a reconnect
				c.mu.Lock()
				if c.conn != nil {
					c.conn.Close()
				}
				c.mu.Unlock()
				return
			}
			c.mu.Lock()
			c.lastAckReceived = false
			c.mu.Unlock()
			if err := c.sendHeartbeat(); err != nil {
				return
			}
		}
	}
}

func (c *Client) sendHeartbeat() error {
	var seq interface{}
	c.mu.Lock()
	if c.seq != nil {
		seq = *c.seq
	}
	c.mu.Unlock()
	return c.send(OpHeartbeat, seq)
}

func (c *Client) send(op int, d interface{}) error {
	data, err := json.Marshal(d)
	if err != nil {
		return err
	}
	p := Payload{Op: op, D: data}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("no active connection")
	}
	return c.conn.WriteJSON(p)
}

func (c *Client) handleDispatch(p Payload) {
	if p.T == nil {
		return
	}
	switch *p.T {
	case "READY":
		var r ReadyData
		if err := json.Unmarshal(p.D, &r); err != nil {
			log.Printf("gateway: bad READY payload: %v", err)
			return
		}
		c.mu.Lock()
		c.sessionID = r.SessionID
		c.resumeURL = r.ResumeGatewayURL
		c.mu.Unlock()
		log.Printf("gateway: ready, session %s", r.SessionID)

	case "MESSAGE_CREATE":
		if c.OnMessageCreate == nil {
			return
		}
		var m MessageCreate
		if err := json.Unmarshal(p.D, &m); err != nil {
			log.Printf("gateway: bad MESSAGE_CREATE payload: %v", err)
			return
		}
		if m.Author.Bot {
			return
		}
		c.OnMessageCreate(m)

	default:
		// Ignore event types we don't care about yet.
	}
}
