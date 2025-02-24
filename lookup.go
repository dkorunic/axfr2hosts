// @license
// Copyright (C) 2024  Dinko Korunic
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"

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
	key := strings.Join([]string{strconv.FormatUint(uint64(t), 10), s}, "")
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
