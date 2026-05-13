// SPDX-FileCopyrightText: 2024 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"net"
	"strconv"

	"github.com/miekg/dns"
	"golang.org/x/sync/singleflight"
)

var lookupGroup singleflight.Group

// lookupFunc is a function that returns a closure function to perform DNS lookups based on the type of the DNS record.
//
// It takes a context.Context, a string, a uint16, and a net.Resolver as parameters, and returns a closure function that
// returns an interface and an error.
func lookupFunc(ctx context.Context, s string, t uint16, r *net.Resolver) func() (any, error) {
	switch t {
	case dns.TypeCNAME:
		return func() (any, error) {
			ctx, cancel := context.WithTimeout(ctx, *resolverTimeout)
			defer cancel()

			rr, err := r.LookupCNAME(ctx, s)

			return []string{rr}, err
		}
	case dns.TypeA, dns.TypeAAAA:
		return func() (any, error) {
			ctx, cancel := context.WithTimeout(ctx, *resolverTimeout)
			defer cancel()

			return r.LookupHost(ctx, s)
		}
	}

	return nil
}

// lookup performs a lookup operation using the provided context, string, type, and resolver.
//
// It returns a slice of strings and an error.
func lookup(ctx context.Context, s string, t uint16, r *net.Resolver) ([]string, error) {
	b := strconv.AppendUint(make([]byte, 0, len(s)+5), uint64(t), 10)
	key := string(append(b, s...))
	ch := lookupGroup.DoChan(key, lookupFunc(ctx, s, t, r))

	var err error

	select {
	case <-ctx.Done():
		err = ctx.Err()
		if errors.Is(err, context.DeadlineExceeded) {
			lookupGroup.Forget(key)

			return nil, err
		}
	case res := <-ch:
		rrs, ok := res.Val.([]string)
		if ok {
			return rrs, res.Err
		}

		return []string{}, res.Err
	}

	return []string{}, nil
}
