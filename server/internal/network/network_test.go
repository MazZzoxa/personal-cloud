package network

import "testing"

func TestIsTailscaleIPv4(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"100.64.0.1", true},
		{"100.121.112.23", true},
		{"100.127.255.254", true},
		{"100.63.255.255", false},
		{"100.128.0.1", false},
		{"192.168.1.50", false},
		{"2001:db8::1", false},
		{"", false},
	}
	for _, test := range tests {
		if got := isTailscaleIPv4(test.ip); got != test.want {
			t.Fatalf("isTailscaleIPv4(%q) = %v, want %v", test.ip, got, test.want)
		}
	}
}
