package main

import (
	"context"
	"net"
	"testing"

	"github.com/miekg/dns"
)

func TestLookupFuncTypes(t *testing.T) {
	ctx := context.Background()
	r := &net.Resolver{PreferGo: true}

	tests := []struct {
		name    string
		t       uint16
		wantNil bool
	}{
		{"TypeCNAME", dns.TypeCNAME, false},
		{"TypeA", dns.TypeA, false},
		{"TypeAAAA", dns.TypeAAAA, false},
		{"TypeMX (unsupported)", dns.TypeMX, true},
		{"TypeNS (unsupported)", dns.TypeNS, true},
		{"TypeSOA (unsupported)", dns.TypeSOA, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := lookupFunc(ctx, "host.example.com.", tt.t, r)
			if tt.wantNil && fn != nil {
				t.Errorf("lookupFunc(%d) = non-nil, want nil", tt.t)
			}
			if !tt.wantNil && fn == nil {
				t.Errorf("lookupFunc(%d) = nil, want non-nil closure", tt.t)
			}
		})
	}
}
