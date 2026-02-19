package tunnel

import (
	"io"

	"github.com/hashicorp/yamux"
)

// NewYamuxConfig returns a yamux configuration tuned for long-lived tunnel
// connections behind cloud load balancers. The keepalive interval ensures
// that idle connections are not dropped by intermediaries (most ALBs/proxies
// use a 60s idle timeout).
func NewYamuxConfig(cfg YamuxConfig) *yamux.Config {
	ycfg := yamux.DefaultConfig()
	ycfg.EnableKeepAlive = true
	ycfg.KeepAliveInterval = cfg.KeepAliveInterval
	ycfg.ConnectionWriteTimeout = cfg.ConnectionWriteTimeout
	ycfg.LogOutput = io.Discard
	return ycfg
}
