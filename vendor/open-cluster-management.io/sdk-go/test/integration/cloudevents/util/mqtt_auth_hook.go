package util

import (
	"bytes"
	"crypto/tls"
	"time"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"k8s.io/klog/v2"
)

type AllowHook struct {
	mqtt.HookBase
}

// ID returns the ID of the hook.
func (h *AllowHook) ID() string {
	return "allow-all-auth"
}

// Provides indicates which hook methods this hook provides.
func (h *AllowHook) Provides(b byte) bool {
	return bytes.Contains([]byte{
		mqtt.OnConnectAuthenticate,
		mqtt.OnACLCheck,
	}, []byte{b})
}

// OnConnectAuthenticate returns true/allowed for all requests.
func (h *AllowHook) OnConnectAuthenticate(cl *mqtt.Client, pk packets.Packet) bool {
	return true
}

// OnACLCheck returns true/allowed for all checks.
func (h *AllowHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	tlsConn, ok := cl.Net.Conn.(*tls.Conn)
	if ok {
		now := time.Now().UTC()
		if err := tlsConn.Handshake(); err != nil {
			klog.Errorf("mqtt handshake failed: %v", err)
			return false
		}
		state := tlsConn.ConnectionState()
		for _, cert := range state.PeerCertificates {
			if cert.Subject.CommonName != "test-client" {
				continue
			}

			if cert.NotAfter.Before(now) {
				klog.Errorf("mqtt client cert expired (NotAfter=%v, Now=%v)\n", cert.NotAfter, now)
				return false
			}
		}

		return true
	}
	return true
}
