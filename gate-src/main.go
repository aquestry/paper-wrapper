package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/robinbraemer/event"
	"go.minekube.com/common/minecraft/color"
	"go.minekube.com/common/minecraft/component"
	"go.minekube.com/common/minecraft/key"
	"go.minekube.com/gate/cmd/gate"
	"go.minekube.com/gate/pkg/edition/java/cookie"
	"go.minekube.com/gate/pkg/edition/java/proxy"
)

var secretKey = []byte("Jeg[e}Mts|;;ZD&Jv2_J5#zMK25(%~aEC0|I,I#u%P13HEr7,lx3y1kPe9DSD>Gp2")

var backendOnline atomic.Bool
var backendOnlineSince atomic.Int64 // unix nano of first “online” ping

const (
	backendHost        = "localhost"
	backendPort        = 25566
	backendPingTimeout = 2 * time.Second
	backendWaitTimeout = 30 * time.Second
	backendWarmupTime  = 3 * time.Second // how long backend must be stable before we let players in
)

// ---------- token stuff ----------

func verifyToken(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", false
	}

	payloadB64 := parts[0]
	sigB64 := parts[1]

	h := hmac.New(sha256.New, secretKey)
	h.Write([]byte(payloadB64))
	expectedSig := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(sigB64), []byte(expectedSig)) {
		return "", false
	}

	raw, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return "", false
	}

	return string(raw), true
}

func parsePayload(payload string) map[string]string {
	out := make(map[string]string)
	for _, part := range strings.Split(payload, ";") {
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			out[kv[0]] = kv[1]
		}
	}
	return out
}

func fail(player proxy.Player, msg string) {
	player.Disconnect(&component.Text{
		Content: msg,
		S: component.Style{
			Color: color.Red,
		},
	})
}

// ---------- Minecraft status ping (server list ping protocol) ----------

type mcStatusPlayers struct {
	Max    int `json:"max"`
	Online int `json:"online"`
}

type mcStatusResponse struct {
	Description any             `json:"description"`
	Players     mcStatusPlayers `json:"players"`
}

func writeVarInt(buf *bytes.Buffer, value int) {
	for {
		if (value & ^0x7F) == 0 {
			buf.WriteByte(byte(value))
			return
		}
		buf.WriteByte(byte(value&0x7F) | 0x80)
		value >>= 7
	}
}

func readVarInt(r io.Reader) (int, error) {
	var numRead int
	var result int
	for {
		var b [1]byte
		_, err := r.Read(b[:])
		if err != nil {
			return 0, err
		}
		val := int(b[0] & 0x7F)
		result |= val << (7 * numRead)

		numRead++
		if numRead > 5 {
			return 0, fmt.Errorf("varint too big")
		}
		if (b[0] & 0x80) == 0 {
			break
		}
	}
	return result, nil
}

func writeString(buf *bytes.Buffer, s string) {
	writeVarInt(buf, len(s))
	buf.WriteString(s)
}

func readString(r io.Reader) (string, error) {
	length, err := readVarInt(r)
	if err != nil {
		return "", err
	}
	if length < 0 {
		return "", fmt.Errorf("negative string length")
	}
	data := make([]byte, length)
	_, err = io.ReadFull(r, data)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func extractMotd(desc any) string {
	switch v := desc.(type) {
	case string:
		return v
	case map[string]any:
		if t, ok := v["text"].(string); ok {
			return t
		}
		return fmt.Sprint(v)
	default:
		return fmt.Sprint(desc)
	}
}

func pingMinecraftStatus(host string, port int, timeout time.Duration) (*mcStatusResponse, time.Duration, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, 0, err
	}
	defer func(conn net.Conn) {
		err := conn.Close()
		if err != nil {

		}
	}(conn)
	_ = conn.SetDeadline(time.Now().Add(timeout))

	const protocolVersion = 760

	handshakeInner := &bytes.Buffer{}
	handshakeInner.WriteByte(0x00)
	writeVarInt(handshakeInner, protocolVersion)
	writeString(handshakeInner, host)
	_ = binary.Write(handshakeInner, binary.BigEndian, uint16(port))
	writeVarInt(handshakeInner, 1)

	handshake := &bytes.Buffer{}
	writeVarInt(handshake, handshakeInner.Len())
	handshake.Write(handshakeInner.Bytes())

	if _, err := conn.Write(handshake.Bytes()); err != nil {
		return nil, 0, err
	}

	req := &bytes.Buffer{}
	reqInner := &bytes.Buffer{}
	reqInner.WriteByte(0x00)
	writeVarInt(req, reqInner.Len())
	req.Write(reqInner.Bytes())

	if _, err := conn.Write(req.Bytes()); err != nil {
		return nil, 0, err
	}

	if _, err := readVarInt(conn); err != nil {
		return nil, 0, err
	}
	if _, err := readVarInt(conn); err != nil {
		return nil, 0, err
	}
	jsonStr, err := readString(conn)
	if err != nil {
		return nil, 0, err
	}

	var status mcStatusResponse
	if err := json.Unmarshal([]byte(jsonStr), &status); err != nil {
		return nil, 0, err
	}

	latency := time.Since(start)
	return &status, latency, nil
}

// ---------- helper: is backend ready for players? ----------

func backendReady() bool {
	if !backendOnline.Load() {
		return false
	}
	since := time.Unix(0, backendOnlineSince.Load())
	// if we never set a timestamp, be conservative
	if since.IsZero() {
		return false
	}
	return time.Since(since) >= backendWarmupTime
}

// ---------- plugin ----------

func main() {
	proxy.Plugins = append(proxy.Plugins, CookieReader)
	gate.Execute()
}

var CookieReader = proxy.Plugin{
	Name: "CookieReader",
	Init: func(ctx context.Context, p *proxy.Proxy) error {
		logger := logr.FromContextOrDiscard(ctx)
		k, _ := key.Parse("master:session")

		backendOnline.Store(false)
		backendOnlineSince.Store(0)

		// ping watcher
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					status, latency, err := pingMinecraftStatus(backendHost, backendPort, backendPingTimeout)
					if err != nil {
						backendOnline.Store(false)
						backendOnlineSince.Store(0)
						logger.Info(
							"MC backend OFFLINE",
							"addr", fmt.Sprintf("%s:%d", backendHost, backendPort),
							"err", err.Error(),
						)
						continue
					}

					// first time it’s online after being offline → set timestamp
					if !backendOnline.Load() {
						backendOnlineSince.Store(time.Now().UnixNano())
					}

					backendOnline.Store(true)

					logger.Info(
						"MC backend ONLINE",
						"addr", fmt.Sprintf("%s:%d", backendHost, backendPort),
						"latency_ms", latency.Milliseconds(),
						"motd", extractMotd(status.Description),
						"players_online", status.Players.Online,
						"players_max", status.Players.Max,
					)
				}
			}
		}()

		// hold player in pre-state while backend is not ready
		event.Subscribe(p.Event(), 0, func(e *proxy.PlayerChooseInitialServerEvent) {
			player := e.Player()
			if player == nil {
				return
			}

			// 1) session / cookie
			ctxReq, cancel := context.WithTimeout(player.Context(), 3*time.Second)
			defer cancel()

			c, err := cookie.Request(ctxReq, player, k, p.Event())
			if err != nil {
				fail(player, "Session validation failed.")
				return
			}
			if c == nil || len(c.Payload) == 0 {
				fail(player, "No session token was provided.")
				return
			}

			token := string(c.Payload)
			payload, ok := verifyToken(token)
			if !ok {
				fail(player, "Invalid session token")
				return
			}

			claims := parsePayload(payload)
			exp, _ := strconv.ParseInt(claims["exp"], 10, 64)

			if exp <= time.Now().Unix() {
				fail(player, "Session expired.")
				return
			}

			log.Printf("[Session] %s authenticated: %s", player.Username(), payload)

			// 2) wait until backend is fully ready (online + warmup), or timeout
			if backendReady() {
				return
			}

			waitCtx, waitCancel := context.WithTimeout(player.Context(), backendWaitTimeout)
			defer waitCancel()

			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()

			for {
				if backendReady() {
					log.Printf("[BackendWait] Backend ready, continuing login for %s", player.Username())
					return
				}

				select {
				case <-waitCtx.Done():
					fail(player, "Your game server is currently starting. Please try again in a moment.")
					return
				case <-ticker.C:
				}
			}
		})

		logger.Info("Cookie session validator + MC localhost:25566 watcher with pre-wait & warmup active")
		return nil
	},
}
