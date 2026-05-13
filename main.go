// SPDX-FileCopyrightText: 2021 Dinko Korunic
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"net/netip"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"go.uber.org/automaxprocs/maxprocs"
)

const (
	mapSize      = 4096
	subMapSize   = 8
	hostChanSize = 2048
	maxMemRatio  = 0.9
)

var (
	GitTag    = ""
	GitCommit = ""
	GitDirty  = ""
	BuildTime = ""
)

// main is the entry point of the application.
func main() {
	_, _ = memlimit.SetGoMemLimitWithOpts(
		memlimit.WithRatio(maxMemRatio),
		memlimit.WithProvider(
			memlimit.ApplyFallback(
				memlimit.FromCgroup,
				memlimit.FromSystem,
			),
		),
	)

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

	_ = setNofile()

	ranger, doCIDR := rangerInit(cidrList)
	hostChan := make(chan HostEntry, hostChanSize)

	entries := make(HostMap, mapSize)
	keys := make([]netip.Addr, 0, mapSize)

	var wgMon, wgWrk sync.WaitGroup

	// host map/key slice managing monitor routine
	wgMon.Go(func() {
		writeHostEntries(hostChan, &keys, entries)
	})

	// limit total AXFRs in progress
	semAXFR := make(chan struct{}, *maxTransfers)

	// routines for processing local and remote zones
	for _, zone := range zones {
		if server == "" {
			// there is no remote server, so assume zones are local Bind9 files
			wgWrk.Go(func() {
				processLocalZone(zone, doCIDR, ranger, hostChan)
			})
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
