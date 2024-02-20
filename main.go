// @license
// Copyright (C) 2021  Dinko Korunic
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
	"fmt"
	"net/netip"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"

	_ "github.com/KimMachineGun/automemlimit"
	"go.uber.org/automaxprocs/maxprocs"
)

const (
	mapSize      = 4096
	subMapSize   = 16
	hostChanSize = 64
)

func main() {
	undo, _ := maxprocs.Set()
	defer undo()

	zones, server, cidrList := parseFlags()

	// enable CPU profiling dump on exit
	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating CPU profile: %v\n", err)
		}
		defer f.Close()

		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting CPU profile: %v\n", err)
		}
		defer pprof.StopCPUProfile()
	}

	// enable memory profile dump on exit
	if *memProfile != "" {
		f, err := os.Create(*memProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error trying to create memory profile: %v\n", err)
		}
		defer f.Close()

		defer func() {
			runtime.GC()

			if err := pprof.WriteHeapProfile(f); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing memory profile: %v\n", err)
			}
		}()
	}

	ranger, doCIDR := rangerInit(cidrList)
	hostChan := make(chan HostEntry, hostChanSize)

	entries := make(HostMap, mapSize)
	keys := make([]netip.Addr, 0, mapSize)

	var wgMon, wgWrk sync.WaitGroup

	wgMon.Add(1)

	// host map/key slice managing monitor routine
	go func() {
		defer wgMon.Done()

		writeHostEntries(hostChan, &keys, entries)
	}()

	// limit total AXFRs in progress
	semAXFR := make(chan struct{}, *maxTransfers)

	// routines for processing local and remote zones
	for _, zone := range zones {
		zone := zone

		if server == "" {
			// there is no remote server, so assume zones are local Bind9 files
			wgWrk.Add(1)

			go func() {
				defer wgWrk.Done()

				processLocalZone(zone, doCIDR, ranger, hostChan)
			}()
		} else {
			// otherwise assume remote AXFR-able zones
			wgWrk.Add(1)
			semAXFR <- struct{}{}

			go func() {
				defer wgWrk.Done()

				processRemoteZone(zone, server, doCIDR, ranger, hostChan)
				<-semAXFR
			}()
		}
	}

	wgWrk.Wait()
	close(hostChan)
	wgMon.Wait()

	displayHostEntries(keys, entries)
}
