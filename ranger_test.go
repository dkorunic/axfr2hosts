package main

import (
	"net/netip"
	"testing"
)

func TestRangerInit(t *testing.T) {
	tests := []struct {
		name         string
		cidrList     []string
		wantDoCIDR   bool
		checkIPs     []netip.Addr
		checkResults []bool
	}{
		{
			name:       "Empty list",
			cidrList:   []string{},
			wantDoCIDR: false,
		},
		{
			name:       "Valid CIDRs",
			cidrList:   []string{"192.0.2.0/24", "2001:db8::/32"},
			wantDoCIDR: true,
			checkIPs: []netip.Addr{
				netip.MustParseAddr("192.0.2.1"),
				netip.MustParseAddr("192.0.2.255"),
				netip.MustParseAddr("192.0.3.1"),
				netip.MustParseAddr("2001:db8::1"),
				netip.MustParseAddr("2001:db9::1"),
			},
			checkResults: []bool{true, true, false, true, false},
		},
		{
			name:       "Invalid CIDR",
			cidrList:   []string{"invalid"},
			wantDoCIDR: true, // It still returns true because list is not empty, but ranger might be empty or partial
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ranger, doCIDR := rangerInit(tt.cidrList)

			if doCIDR != tt.wantDoCIDR {
				t.Errorf("rangerInit() doCIDR = %v, want %v", doCIDR, tt.wantDoCIDR)
			}

			if ranger != nil && len(tt.checkIPs) > 0 {
				for i, ip := range tt.checkIPs {
					contains, err := ranger.Contains(ip)
					if err != nil {
						t.Errorf("ranger.Contains(%v) error = %v", ip, err)
					}
					if contains != tt.checkResults[i] {
						t.Errorf("ranger.Contains(%v) = %v, want %v", ip, contains, tt.checkResults[i])
					}
				}
			}
		})
	}
}
