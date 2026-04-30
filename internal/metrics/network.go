package metrics

import (
	"context"
	"time"

	psnet "github.com/shirou/gopsutil/v3/net"
)

// NetworkSnapshot summarises active TCP/UDP connections.
type NetworkSnapshot struct {
	CollectedAt    time.Time        `json:"collected_at"`
	ActiveConns    int              `json:"active_connections"`
	TCPEstablished int              `json:"tcp_established"`
	TCPListening   int              `json:"tcp_listening"`
	Connections    []ConnStat       `json:"connections"`
}

type ConnStat struct {
	Proto      string `json:"proto"`
	LocalAddr  string `json:"local_addr"`
	RemoteAddr string `json:"remote_addr"`
	State      string `json:"state"`
}

func CollectNetwork(ctx context.Context) (*NetworkSnapshot, error) {
	snap := &NetworkSnapshot{CollectedAt: time.Now()}

	conns, err := psnet.ConnectionsWithContext(ctx, "all")
	if err != nil {
		return snap, nil // graceful degradation
	}

	snap.ActiveConns = len(conns)
	const maxDetail = 100
	for i, c := range conns {
		switch c.Status {
		case "ESTABLISHED":
			snap.TCPEstablished++
		case "LISTEN":
			snap.TCPListening++
		}
		if i < maxDetail {
			laddr := ""
			raddr := ""
			if c.Laddr.IP != "" {
				laddr = c.Laddr.IP + ":" + itoa(c.Laddr.Port)
			}
			if c.Raddr.IP != "" {
				raddr = c.Raddr.IP + ":" + itoa(c.Raddr.Port)
			}
			proto := "tcp"
			if c.Type == 2 {
				proto = "udp"
			}
			snap.Connections = append(snap.Connections, ConnStat{
				Proto:      proto,
				LocalAddr:  laddr,
				RemoteAddr: raddr,
				State:      c.Status,
			})
		}
	}
	return snap, nil
}

func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	buf := [10]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte(n%10) + '0'
		n /= 10
	}
	return string(buf[pos:])
}
