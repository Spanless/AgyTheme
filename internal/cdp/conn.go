package cdp

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

type Message struct {
	ID     int             `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (m *Message) EvalValue() any {
	if m == nil || len(m.Result) == 0 {
		return nil
	}
	var r struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(m.Result, &r) != nil {
		return nil
	}
	return r.Result.Value
}

func (m *Message) ExceptionText() (string, bool) {
	if m == nil || len(m.Result) == 0 {
		return "", false
	}
	var r struct {
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if json.Unmarshal(m.Result, &r) != nil || r.ExceptionDetails == nil {
		return "", false
	}
	if r.ExceptionDetails.Exception != nil && r.ExceptionDetails.Exception.Description != "" {
		return r.ExceptionDetails.Exception.Description, true
	}
	return r.ExceptionDetails.Text, true
}

type Conn struct {
	ws      *wsConn
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	nextID  int
	pending map[int]chan *Message
	events  chan *Message
	done    chan struct{}
}

const SendTimeout = 2500 * time.Millisecond

func Dial(ctx context.Context, wsURL string) (*Conn, error) {
	ws, err := wsDial(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithCancel(context.Background())
	c := &Conn{
		ws:      ws,
		ctx:     cctx,
		cancel:  cancel,
		nextID:  1,
		pending: map[int]chan *Message{},
		events:  make(chan *Message, 512),
		done:    make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

func (c *Conn) readLoop() {
	defer func() {
		c.mu.Lock()
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
		c.mu.Unlock()
		close(c.events)
		close(c.done)
		c.cancel()
	}()
	for {
		data, err := c.ws.Read()
		if err != nil {
			return
		}
		var msg Message
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.ID != 0 {
			c.mu.Lock()
			ch, ok := c.pending[msg.ID]
			if ok {
				delete(c.pending, msg.ID)
			}
			c.mu.Unlock()
			if ok {
				ch <- &msg
			}
			continue
		}
		if msg.Method != "" {
			select {
			case c.events <- &msg:
			default:
			}
		}
	}
}

func (c *Conn) Events() <-chan *Message { return c.events }

func (c *Conn) Done() <-chan struct{} { return c.done }

func (c *Conn) Send(method string, params any) *Message {
	if params == nil {
		params = struct{}{}
	}
	select {
	case <-c.done:
		return nil
	default:
	}
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	ch := make(chan *Message, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	data, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		c.drop(id)
		return nil
	}
	wctx, cancel := context.WithTimeout(c.ctx, SendTimeout)
	defer cancel()
	deadline, _ := wctx.Deadline()
	if err := c.ws.WriteText(data, deadline); err != nil {
		c.drop(id)
		return nil
	}
	select {
	case msg := <-ch:
		return msg
	case <-wctx.Done():
		c.drop(id)
		return nil
	}
}

func (c *Conn) drop(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Conn) Close() {
	_ = c.ws.Close()
	c.cancel()
}
