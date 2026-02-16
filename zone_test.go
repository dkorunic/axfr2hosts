package main

import (
	"net/netip"
	"testing"
)

func TestUnmapAddrFromSlice(t *testing.T) {
	tests := []struct {
		name     string
		slice    []byte
		expected netip.Addr
		ok       bool
	}{
		{
			name:     "IPv4 address",
			slice:    []byte{192, 0, 2, 1},
			expected: netip.MustParseAddr("192.0.2.1"),
			ok:       true,
		},
		{
			name:     "IPv6 address",
			slice:    []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
			expected: netip.MustParseAddr("2001:db8::1"),
			ok:       true,
		},
		{
			name:     "IPv4-mapped IPv6 address",
			slice:    []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 192, 0, 2, 1},
			expected: netip.MustParseAddr("192.0.2.1"),
			ok:       true,
		},
		{
			name:     "Invalid slice length",
			slice:    []byte{1, 2, 3},
			expected: netip.Addr{},
			ok:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := unmapAddrFromSlice(tt.slice)
			if ok != tt.ok {
				t.Errorf("unmapAddrFromSlice() ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.expected {
				t.Errorf("unmapAddrFromSlice() got = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestUnmapParseAddr(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected netip.Addr
		wantErr  bool
	}{
		{
			name:     "IPv4 address",
			input:    "192.0.2.1",
			expected: netip.MustParseAddr("192.0.2.1"),
			wantErr:  false,
		},
		{
			name:     "IPv6 address",
			input:    "2001:db8::1",
			expected: netip.MustParseAddr("2001:db8::1"),
			wantErr:  false,
		},
		{
			name:     "IPv4-mapped IPv6 address",
			input:    "::ffff:192.0.2.1",
			expected: netip.MustParseAddr("192.0.2.1"),
			wantErr:  false,
		},
		{
			name:     "Invalid address",
			input:    "invalid",
			expected: netip.Addr{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := unmapParseAddr(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("unmapParseAddr() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.expected {
				t.Errorf("unmapParseAddr() got = %v, want %v", got, tt.expected)
			}
		})
	}
}
