package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"log"
	"strconv"
	"strings"
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

func main() {
	proxy.Plugins = append(proxy.Plugins, CookieReader)
	gate.Execute()
}

var CookieReader = proxy.Plugin{
	Name: "CookieReader",
	Init: func(ctx context.Context, p *proxy.Proxy) error {
		logger := logr.FromContextOrDiscard(ctx)
		k, _ := key.Parse("master:session")

		event.Subscribe(p.Event(), 0, func(e *proxy.PlayerChooseInitialServerEvent) {
			player := e.Player()
			if player == nil {
				return
			}

			ctx, cancel := context.WithTimeout(player.Context(), 3*time.Second)
			defer cancel()

			c, err := cookie.Request(ctx, player, k, p.Event())
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
		})

		logger.Info("Cookie session validator active")
		return nil
	},
}
