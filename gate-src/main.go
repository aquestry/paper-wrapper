package main

import (
	"context"
	"log"
	"time"

	"github.com/go-logr/logr"
	"github.com/robinbraemer/event"
	"go.minekube.com/common/minecraft/key"
	"go.minekube.com/gate/cmd/gate"
	"go.minekube.com/gate/pkg/edition/java/cookie"
	"go.minekube.com/gate/pkg/edition/java/proxy"
)

func main() {
	proxy.Plugins = append(proxy.Plugins, CookieReader)
	gate.Execute()
}

var CookieReader = proxy.Plugin{
	Name: "CookieReader",
	Init: func(ctx context.Context, p *proxy.Proxy) error {
		logger := logr.FromContextOrDiscard(ctx)

		k, _ := key.Parse("master:session")

		event.Subscribe(p.Event(), 0, func(e *proxy.ServerPostConnectEvent) {
			player := e.Player()
			if player == nil {
				return
			}

			ctx, cancel := context.WithTimeout(player.Context(), 3*time.Second)
			defer cancel()

			c, err := cookie.Request(ctx, player, k, p.Event())
			if err != nil {
				log.Printf("[CookieReader] %s request failed: %v", player.Username(), err)
				return
			}
			if c == nil || len(c.Payload) == 0 {
				log.Printf("[CookieReader] %s cookie empty", player.Username())
				return
			}

			log.Printf("[CookieReader] %s cookie: %s", player.Username(), string(c.Payload))
		})

		logger.Info("CookieReader initialized")
		return nil
	},
}
