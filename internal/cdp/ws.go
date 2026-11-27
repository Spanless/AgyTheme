package cdp

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Spanless/AgyTheme/internal/shared"
)

type wsConn struct {
	nc        net.Conn
	br        *bufio.Reader
	wmu       sync.Mutex
	readLimit int64
}

const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

func wsDial(ctx context.Context, rawURL string) (*wsConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" {
		return nil, fmt.Errorf("unsupported websocket scheme %q", u.Scheme)
	}
	if u.User != nil || !shared.IsLoopbackHost(u.Hostname()) {
		return nil, fmt.Errorf("refusing non-loopback websocket host %q", u.Hostname())
	}
	reqURI := u.RequestURI()
	if strings.ContainsAny(u.Host+reqURI, "\r\n") {
		return nil, errors.New("invalid websocket url")
	}
	var d net.Dialer
	nc, err := d.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = nc.SetDeadline(dl)
	}
	keyRaw := make([]byte, 16)
	_, _ = rand.Read(keyRaw)
	key := base64.StdEncoding.EncodeToString(keyRaw)
	req := "GET " + reqURI + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(nc, req); err != nil {
		nc.Close()
		return nil, err
	}
	br := bufio.NewReaderSize(nc, 64<<10)
	status, err := br.ReadString('\n')
	if err != nil {
		nc.Close()
		return nil, err
	}
	if f := strings.Fields(status); len(f) < 2 || f[1] != "101" {
		nc.Close()
		return nil, fmt.Errorf("websocket handshake: %s", strings.TrimSpace(status))
	}
	accept := ""
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			nc.Close()
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Sec-WebSocket-Accept") {
			accept = strings.TrimSpace(v)
		}
	}
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if accept != base64.StdEncoding.EncodeToString(sum[:]) {
		nc.Close()
		return nil, errors.New("websocket handshake: bad Sec-WebSocket-Accept")
	}
	_ = nc.SetDeadline(time.Time{})
	return &wsConn{nc: nc, br: br, readLimit: 64 << 20}, nil
}

func (c *wsConn) Read() ([]byte, error) {
	var msg []byte
	for {
		fin, op, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch op {
		case opPing:
			if err := c.writeFrame(opPong, payload, time.Now().Add(SendTimeout)); err != nil {
				return nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			_ = c.writeFrame(opClose, nil, time.Now().Add(time.Second))
			return nil, io.EOF
		}
		if int64(len(msg)+len(payload)) > c.readLimit {
			return nil, errors.New("websocket message too large")
		}
		msg = append(msg, payload...)
		if fin {
			return msg, nil
		}
	}
}

func (c *wsConn) readFrame() (fin bool, op byte, payload []byte, err error) {
	var h [2]byte
	if _, err = io.ReadFull(c.br, h[:]); err != nil {
		return
	}
	fin = h[0]&0x80 != 0
	op = h[0] & 0x0F
	masked := h[1]&0x80 != 0
	n := int64(h[1] & 0x7F)
	switch n {
	case 126:
		var b [2]byte
		if _, err = io.ReadFull(c.br, b[:]); err != nil {
			return
		}
		n = int64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err = io.ReadFull(c.br, b[:]); err != nil {
			return
		}
		n = int64(binary.BigEndian.Uint64(b[:]))
	}
	if n < 0 || n > c.readLimit {
		err = errors.New("websocket frame too large")
		return
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return
		}
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	if op != opContinuation && op != opText && op != opBinary && op != opClose && op != opPing && op != opPong {
		err = fmt.Errorf("websocket opcode %d", op)
	}
	return
}

func (c *wsConn) WriteText(data []byte, deadline time.Time) error {
	return c.writeFrame(opText, data, deadline)
}

func (c *wsConn) writeFrame(op byte, data []byte, deadline time.Time) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	buf := make([]byte, 0, 14+len(data))
	buf = append(buf, 0x80|op)
	switch n := len(data); {
	case n < 126:
		buf = append(buf, 0x80|byte(n))
	case n <= 0xFFFF:
		buf = append(buf, 0x80|126)
		buf = binary.BigEndian.AppendUint16(buf, uint16(n))
	default:
		buf = append(buf, 0x80|127)
		buf = binary.BigEndian.AppendUint64(buf, uint64(n))
	}
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	buf = append(buf, mask[:]...)
	start := len(buf)
	buf = append(buf, data...)
	for i := range data {
		buf[start+i] ^= mask[i%4]
	}
	_ = c.nc.SetWriteDeadline(deadline)
	_, err := c.nc.Write(buf)
	return err
}

func (c *wsConn) Close() error {
	_ = c.writeFrame(opClose, []byte{0x03, 0xE8}, time.Now().Add(200*time.Millisecond))
	return c.nc.Close()
}
