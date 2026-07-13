package platform

import (
	"fmt"
	"net"
	"slices"
	"strings"
)

type lanInterface struct {
	name  string
	flags net.Flags
	addrs []net.Addr
}

type lanCandidate struct {
	ip   net.IP
	rank int
}

const (
	rankVirtual         = 4
	rankNotDefaultRoute = 2
	rankNotPrivate      = 1
)

func LANAddrs() ([]net.IP, error) {
	ifaces, err := systemInterfaces()
	if err != nil {
		return nil, err
	}
	return lanAddrs(ifaces, defaultRouteIP()), nil
}

func systemInterfaces() ([]lanInterface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	listed := make([]lanInterface, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("list addresses of %s: %w", iface.Name, err)
		}
		listed = append(listed, lanInterface{name: iface.Name, flags: iface.Flags, addrs: addrs})
	}
	return listed, nil
}

func defaultRouteIP() net.IP {
	probe := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 9}
	conn, err := net.DialUDP("udp4", nil, probe)
	if err != nil {
		return nil
	}
	defer conn.Close()
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil
	}
	return local.IP.To4()
}

func lanAddrs(ifaces []lanInterface, defaultRoute net.IP) []net.IP {
	var candidates []lanCandidate
	for _, iface := range ifaces {
		if iface.flags&net.FlagUp == 0 || iface.flags&net.FlagLoopback != 0 {
			continue
		}
		for _, addr := range iface.addrs {
			if ip := lanIPv4(addr); ip != nil {
				candidates = append(candidates, lanCandidate{ip: ip, rank: addrRank(ip, iface, defaultRoute)})
			}
		}
	}
	slices.SortStableFunc(candidates, func(a, b lanCandidate) int {
		return a.rank - b.rank
	})
	ips := make([]net.IP, 0, len(candidates))
	for _, c := range candidates {
		ips = append(ips, c.ip)
	}
	return ips
}

func addrRank(ip net.IP, iface lanInterface, defaultRoute net.IP) int {
	rank := 0
	if isVirtual(iface) {
		rank += rankVirtual
	}
	if !ip.Equal(defaultRoute) {
		rank += rankNotDefaultRoute
	}
	if !ip.IsPrivate() {
		rank += rankNotPrivate
	}
	return rank
}

func isVirtual(iface lanInterface) bool {
	if iface.flags&net.FlagPointToPoint != 0 {
		return true
	}
	name := strings.ToLower(iface.name)
	for _, prefix := range []string{
		"vethernet", "hyper-v", "virtualbox", "vmware", "vmnet", "vboxnet", "docker", "br-", "veth",
		"virbr", "utun", "tun", "tap", "wg", "tailscale", "zerotier", "zt",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func lanIPv4(addr net.Addr) net.IP {
	var ip net.IP
	switch a := addr.(type) {
	case *net.IPNet:
		ip = a.IP
	case *net.IPAddr:
		ip = a.IP
	default:
		return nil
	}
	ip = ip.To4()
	if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return nil
	}
	return ip
}
