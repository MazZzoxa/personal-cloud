package network

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const tailscaleIPv4Prefix = "100.64.0.0/10"

type Route struct {
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	URL       string `json:"url"`
	Address   string `json:"address"`
	Available bool   `json:"available"`
}

func Discover(port int, scheme string) (lan []string, tailscale []string) {
	lan = lanIPv4s()
	tailscale = tailscaleIPv4s()
	lan = uniqueSorted(lan)
	tailscale = uniqueSorted(tailscale)

	// Keep Tailscale addresses out of the LAN list even if the platform
	// reports the Tailscale interface alongside ordinary network adapters.
	filteredLAN := make([]string, 0, len(lan))
	for _, ip := range lan {
		if !isTailscaleIPv4(ip) {
			filteredLAN = append(filteredLAN, ip)
		}
	}
	return filteredLAN, tailscale
}

func Routes(port int, scheme string) []Route {
	lan, tailscale := Discover(port, scheme)
	routes := make([]Route, 0, 2)

	if len(lan) > 0 {
		ip := lan[0]
		routes = append(routes, Route{
			Kind:      "lan",
			Label:     "Локальная сеть",
			URL:       baseURL(scheme, ip, port),
			Address:   ip,
			Available: true,
		})
	}
	if len(tailscale) > 0 {
		ip := tailscale[0]
		routes = append(routes, Route{
			Kind:      "tailscale",
			Label:     "Tailscale",
			URL:       baseURL(scheme, ip, port),
			Address:   ip,
			Available: true,
		})
	}
	return routes
}

func baseURL(scheme, ip string, port int) string {
	return scheme + "://" + net.JoinHostPort(ip, formatPort(port))
}

func formatPort(port int) string {
	return strconv.Itoa(port)
}

func lanIPv4s() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var result []string
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip := addressIPv4(address)
			if ip == "" || net.ParseIP(ip).IsLinkLocalUnicast() {
				continue
			}
			result = append(result, ip)
		}
	}
	return result
}

func tailscaleIPv4s() []string {
	if output, ok := tailscaleCLIIPv4(); ok {
		return []string{output}
	}

	// Fallback for installations where tailscale.exe is not on PATH.
	// Tailscale uses 100.64.0.0/10 by default for node addresses.
	var result []string
	for _, ip := range lanIPv4s() {
		if isTailscaleIPv4(ip) {
			result = append(result, ip)
		}
	}
	return result
}

func tailscaleCLIIPv4() (string, bool) {
	command := tailscaleCommand()
	if command == "" {
		return "", false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, "ip", "-4")
	output, err := cmd.Output()
	if err != nil || ctx.Err() != nil {
		return "", false
	}

	for _, field := range strings.Fields(string(output)) {
		if net.ParseIP(field) != nil && isTailscaleIPv4(field) {
			return field, true
		}
	}
	return "", false
}

func tailscaleCommand() string {
	if path, err := exec.LookPath("tailscale"); err == nil {
		return path
	}
	if path, err := exec.LookPath("tailscale.exe"); err == nil {
		return path
	}

	// Best-effort check of a common Windows installation path. The interface
	// scan below remains the fallback when the CLI is unavailable.
	if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
		candidate := filepath.Join(programFiles, "Tailscale", "tailscale.exe")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func addressIPv4(address net.Addr) string {
	switch value := address.(type) {
	case *net.IPNet:
		if ip := value.IP.To4(); ip != nil {
			return ip.String()
		}
	case *net.IPAddr:
		if ip := value.IP.To4(); ip != nil {
			return ip.String()
		}
	}
	return ""
}

func isTailscaleIPv4(value string) bool {
	ip := net.ParseIP(value)
	if ip == nil {
		return false
	}
	ip = ip.To4()
	if ip == nil {
		return false
	}
	_, network, err := net.ParseCIDR(tailscaleIPv4Prefix)
	return err == nil && network.Contains(ip)
}

func uniqueSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
