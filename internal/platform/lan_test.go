package platform

import (
	"net"
	"slices"
	"testing"
)

func ipNet(t *testing.T, s string) *net.IPNet {
	t.Helper()
	ip, network, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("parse cidr %s: %v", s, err)
	}
	network.IP = ip
	return network
}

func TestLANAddrs(t *testing.T) {
	t.Parallel()
	upBroadcast := net.FlagUp | net.FlagBroadcast | net.FlagRunning
	tests := []struct {
		name         string
		ifaces       []lanInterface
		defaultRoute string
		want         []string
	}{
		{name: "no interfaces", ifaces: nil, want: nil},
		{
			name: "private addresses come first and keep interface order",
			ifaces: []lanInterface{
				{flags: upBroadcast, addrs: []net.Addr{ipNet(t, "100.101.102.103/32")}},
				{flags: upBroadcast, addrs: []net.Addr{ipNet(t, "192.168.1.20/24")}},
				{flags: upBroadcast, addrs: []net.Addr{ipNet(t, "203.0.113.9/24"), ipNet(t, "10.0.0.5/8")}},
				{flags: upBroadcast, addrs: []net.Addr{ipNet(t, "172.16.4.2/12")}},
			},
			want: []string{"192.168.1.20", "10.0.0.5", "172.16.4.2", "100.101.102.103", "203.0.113.9"},
		},
		{
			name: "the default route address comes before other private addresses",
			ifaces: []lanInterface{
				{name: "Ethernet", flags: upBroadcast, addrs: []net.Addr{ipNet(t, "10.0.0.5/8")}},
				{name: "Wi-Fi", flags: upBroadcast, addrs: []net.Addr{ipNet(t, "192.168.1.20/24")}},
			},
			defaultRoute: "192.168.1.20",
			want:         []string{"192.168.1.20", "10.0.0.5"},
		},
		{
			name: "virtual adapters come after physical ones",
			ifaces: []lanInterface{
				{name: "vEthernet (WSL)", flags: upBroadcast, addrs: []net.Addr{ipNet(t, "172.29.16.1/20")}},
				{name: "vEthernet (Default Switch)", flags: upBroadcast, addrs: []net.Addr{ipNet(t, "172.20.0.1/20")}},
				{name: "VirtualBox Host-Only Network", flags: upBroadcast, addrs: []net.Addr{ipNet(t, "192.168.56.1/24")}},
				{name: "docker0", flags: upBroadcast, addrs: []net.Addr{ipNet(t, "172.17.0.1/16")}},
				{name: "Wi-Fi", flags: upBroadcast, addrs: []net.Addr{ipNet(t, "192.168.1.20/24")}},
			},
			want: []string{"192.168.1.20", "172.29.16.1", "172.20.0.1", "192.168.56.1", "172.17.0.1"},
		},
		{
			name: "a point-to-point vpn holding the default route comes after the lan",
			ifaces: []lanInterface{
				{name: "Corp VPN", flags: net.FlagUp | net.FlagPointToPoint, addrs: []net.Addr{ipNet(t, "10.8.0.6/32")}},
				{name: "Wi-Fi", flags: upBroadcast, addrs: []net.Addr{ipNet(t, "192.168.1.20/24")}},
			},
			defaultRoute: "10.8.0.6",
			want:         []string{"192.168.1.20", "10.8.0.6"},
		},
		{
			name: "a virtual adapter holding the default route leads the virtual ones",
			ifaces: []lanInterface{
				{name: "vEthernet (WSL)", flags: upBroadcast, addrs: []net.Addr{ipNet(t, "172.29.16.1/20")}},
				{name: "vEthernet (External Switch)", flags: upBroadcast, addrs: []net.Addr{ipNet(t, "192.168.1.20/24")}},
			},
			defaultRoute: "192.168.1.20",
			want:         []string{"192.168.1.20", "172.29.16.1"},
		},
		{
			name: "an unknown default route changes nothing",
			ifaces: []lanInterface{
				{name: "Ethernet", flags: upBroadcast, addrs: []net.Addr{ipNet(t, "10.0.0.5/8")}},
				{name: "Wi-Fi", flags: upBroadcast, addrs: []net.Addr{ipNet(t, "192.168.1.20/24")}},
			},
			defaultRoute: "203.0.113.1",
			want:         []string{"10.0.0.5", "192.168.1.20"},
		},
		{
			name: "down interfaces are skipped",
			ifaces: []lanInterface{
				{flags: net.FlagBroadcast, addrs: []net.Addr{ipNet(t, "192.168.1.20/24")}},
				{flags: upBroadcast, addrs: []net.Addr{ipNet(t, "192.168.1.21/24")}},
			},
			want: []string{"192.168.1.21"},
		},
		{
			name: "loopback interfaces and addresses are skipped",
			ifaces: []lanInterface{
				{flags: net.FlagUp | net.FlagLoopback, addrs: []net.Addr{ipNet(t, "127.0.0.1/8"), ipNet(t, "10.9.9.9/8")}},
				{flags: upBroadcast, addrs: []net.Addr{ipNet(t, "127.0.0.2/8"), ipNet(t, "10.0.0.7/8")}},
			},
			want: []string{"10.0.0.7"},
		},
		{
			name: "link-local and ipv6 addresses are skipped",
			ifaces: []lanInterface{
				{flags: upBroadcast, addrs: []net.Addr{
					ipNet(t, "169.254.10.10/16"),
					ipNet(t, "fe80::1/64"),
					ipNet(t, "fd00::5/64"),
					ipNet(t, "2001:db8::1/64"),
					ipNet(t, "192.168.0.2/24"),
				}},
			},
			want: []string{"192.168.0.2"},
		},
		{
			name: "plain ip addresses are accepted",
			ifaces: []lanInterface{
				{flags: upBroadcast, addrs: []net.Addr{&net.IPAddr{IP: net.ParseIP("192.168.5.5")}}},
			},
			want: []string{"192.168.5.5"},
		},
		{
			name: "unspecified address is skipped",
			ifaces: []lanInterface{
				{flags: upBroadcast, addrs: []net.Addr{&net.IPAddr{IP: net.IPv4zero}}},
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, ip := range lanAddrs(tt.ifaces, net.ParseIP(tt.defaultRoute)) {
				if len(ip) != net.IPv4len {
					t.Fatalf("got %d-byte address %v, want 4-byte IPv4", len(ip), ip)
				}
				got = append(got, ip.String())
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDefaultRouteIPIsALocalIPv4AddressWhenThereIsARoute(t *testing.T) {
	t.Parallel()
	ip := defaultRouteIP()
	if ip == nil {
		t.Skip("no default route on this machine")
	}
	if len(ip) != net.IPv4len || ip.IsUnspecified() || ip.IsLoopback() {
		t.Fatalf("got %v, want a usable local IPv4 address", ip)
	}
}

func TestLANAddrsReadsSystemInterfaces(t *testing.T) {
	t.Parallel()
	ips, err := LANAddrs()
	if err != nil {
		t.Fatalf("lan addrs: %v", err)
	}
	for _, ip := range ips {
		if ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			t.Fatalf("got non-LAN address %v", ip)
		}
	}
}
