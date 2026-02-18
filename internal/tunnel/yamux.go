package tunnel

import (
	"io"
	"time"

	"github.com/hashicorp/yamux"
)

// newYamuxConfig returns a yamux configuration tuned for long-lived tunnel
// connections behind cloud load balancers. The 15s keepalive interval ensures
// that idle connections are not dropped by intermediaries (most ALBs/proxies
// use a 60s idle timeout).
func newYamuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 15 * time.Second
	cfg.ConnectionWriteTimeout = 10 * time.Second
	cfg.LogOutput = io.Discard
	return cfg
}
