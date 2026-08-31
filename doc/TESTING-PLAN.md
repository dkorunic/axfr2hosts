# axfr2hosts — Mutation Testing Plan

A catalogue of realistic defects that can be injected into the production code, to
measure whether the test suite actually detects them. High statement coverage says
every line *ran*; it says nothing about whether any assertion would fail if a line
were wrong. This document exists to find that out.

**Inventory: 117 mutations** — 91 Silent, 18 Loud, 8 Control.

| File | Count | | File | Count |
|------|-------|-|------|-------|
| `zone.go` | 32 | | `output.go` | 12 |
| `options.go` | 23 | | `lookup.go` | 11 |
| `axfr.go` | 10 | | `hosts.go` | 10 |
| `main.go` | 10 | | `ranger.go` | 5 |
| `rlimit_unix.go` | 3 | | `init.go` | 1 |

Line numbers refer to the current working tree and were verified against it. They will
drift as the code changes — the code excerpt in each entry is the authoritative locator.

---

## Bias disclosure — read first

This plan was written by an author who had **already read and written large parts of
the test suite** in the same session. That is a real threat to the validity of the
exercise, and it is disclosed here so results are interpreted accordingly.

Two mitigations were applied:

1. **Mechanical enumeration.** Mutations were derived by walking each source file
   line by line and applying a fixed catalogue of mutation operators (below), rather
   than by intuition about "what might be untested". Systematic enumeration is much
   harder to bias than free association.
2. **No detection predictions.** No entry states or hints at whether the suite is
   expected to catch it. That prediction is precisely where author bias would do the
   most damage, and withholding it keeps the measurement meaningful.

For a genuinely clean-room catalogue, regenerate this document from a fresh context
that has been given only the non-test sources.

---

## Mutation operator catalogue

Every entry below is an instance of one of these:

| Op | Name | Description |
|----|------|-------------|
| O1 | Conditional boundary | `<` ↔ `<=`, `>` ↔ `>=`, `==` ↔ `!=` |
| O2 | Condition negation | insert or remove `!`, invert an `if` |
| O3 | Logical connector | `&&` ↔ `\|\|` |
| O4 | Guard removal | delete an early `return`/`continue` or a nil/empty check |
| O5 | Variable substitution | use a different in-scope value of the same type |
| O6 | Normalisation removal | drop `ToLower`, `TrimSuffix`, `Fqdn`, `Unmap`, sort |
| O7 | Constant alteration | change a literal or default |
| O8 | Statement reorder | move a statement across a synchronisation point |
| O9 | Synchronisation removal | drop `Wait`, `defer`, semaphore release |
| O10 | Error-handling change | swallow an error, return the wrong value, `continue` ↔ `break` |
| O11 | Call substitution | swap one library call for a plausible neighbour |

## Severity classes

| Class | Meaning |
|-------|---------|
| **Silent** | Produces wrong output with no error, crash, or diagnostic. The most dangerous class, and the one a suite most needs to catch. |
| **Loud** | Panics, hangs, deadlocks, or emits a visible diagnostic. Easier to catch, but a suite that misses these is badly wrong. |
| **Control** | Semantically equivalent or near-equivalent. **A test failing here is a finding too** — it indicates over-specification or assertions coupled to incidental detail rather than behaviour. |

Controls are deliberately mixed in. A suite that catches 100% of real mutations *and*
100% of controls is brittle, not excellent.

---

## Workflow

Work one mutation at a time. Suggested loop:

```sh
# 1. confirm a clean baseline
go test ./... && go build ./...

# 2. apply exactly one mutation (edit by hand, or keep patches in /tmp)
#    record the diff so it can be reproduced
git diff > /tmp/mutation-M-XXX-NN.patch

# 3. observe
go test ./... 2>&1 | tail -30          # full suite
go test -race ./... 2>&1 | tail -10    # for concurrency mutations
go test -short ./... 2>&1 | tail -5    # does the fast path still catch it?

# 4. revert, verify clean
git checkout -- <file> && go test ./... >/dev/null && echo CLEAN
```

For each case record: **detected?** (yes/no), **which test(s) failed**, **was the
failure message diagnostic** (did it point at the real defect, or just say "want 3 got
2"?), and **did `-short` still catch it**.

A useful extra signal: for mutations the suite *does* catch, check whether it was
caught by a test that was *aiming* at that behaviour, or incidentally by an unrelated
test. Incidental catches are fragile — they disappear the moment that other test is
rewritten.

### Results template

| ID | Detected | Failing test(s) | Message diagnostic? | Caught by `-short`? | Notes |
|----|----------|-----------------|---------------------|---------------------|-------|
| M-OPT-01 | | | | | |

---

## 1. `options.go`

### Flag and limit handling

**M-OPT-01** · `atLeastOne`, line ~151 · O1 · **Loud**
```go
- if n < 1 {
+ if n < 0 {          // uint: never true, clamp never fires
```
*Realistic because:* someone "tidying" a bounds check to the signed-integer idiom
without noticing the parameter is `uint`.
*Symptom:* the clamp becomes dead code; zero-valued limits reach the semaphore and the
retry library.

**M-OPT-02** · `atLeastOne`, line ~152 · O7 · **Loud**
```go
- return 1
+ return 0
```
*Realistic because:* copy-paste or an off-by-one when writing the clamp.
*Symptom:* identical to M-OPT-01 — the function no longer clamps.

**M-OPT-03** · `atLeastOne`, line ~151 · O2 · **Silent**
```go
- if n < 1 {
+ if n > 1 {
```
*Realistic because:* inverted comparison, a classic typo.
*Symptom:* every limit is forced to 1 — all transfers serialise, retries never happen.
No error, just degraded behaviour.

**M-OPT-04** · `normalizeAddrPort`, lines ~92–94 · O11 · **Silent**
```go
- if _, _, err := net.SplitHostPort(addr); err == nil {
-     return addr
- }
+ if strings.Contains(addr, ":") {
+     return addr
+ }
```
*Realistic because:* this is the original implementation; a reviewer might "simplify"
back to it, and it reads as obviously correct.
*Symptom:* a bare IPv6 address already contains `:`, so it never receives a port and is
undialable. Every lookup through that resolver fails.

**M-OPT-05** · `normalizeAddrPort`, line ~92 · O2 · **Silent**
```go
- if _, _, err := net.SplitHostPort(addr); err == nil {
+ if _, _, err := net.SplitHostPort(addr); err != nil {
```
*Symptom:* the logic inverts — addresses that already have a port are re-processed and
addresses lacking one are returned unchanged. Nothing ever gets `:53`.

**M-OPT-06** · `normalizeAddrPort`, lines ~99–103 · O4 · **Silent**
```go
- if inner, ok := strings.CutPrefix(addr, "["); ok {
-     if inner, ok := strings.CutSuffix(inner, "]"); ok {
-         addr = inner
-     }
- }
```
*Realistic because:* the block looks like defensive noise; a reviewer could delete it as
redundant.
*Symptom:* a bracketed IPv6 literal with no port (`[::1]`) becomes `[[::1]]:53`.

**M-OPT-07** · `normalizeAddrPort`, line ~100 · O7 · **Silent**
```go
- if inner, ok := strings.CutSuffix(inner, "]"); ok {
+ if inner, ok := strings.CutSuffix(inner, "["); ok {
```
*Symptom:* bracket unwrapping never matches; same outcome as M-OPT-06.

**M-OPT-08** · `parseZoneArgs`, line ~127 · O11 · **Silent**
```go
- arg = strings.TrimRight(arg, endingDot)
+ arg = strings.TrimSuffix(arg, endingDot)
```
*Realistic because:* `TrimSuffix` is the more common call and reads equivalently; the
difference only shows on repeated dots.
*Symptom:* `example.com..` survives as a zone distinct from `example.com`, defeating
deduplication.

**M-OPT-09** · `parseZoneArgs`, lines ~131–133 · O4 · **Silent**
```go
- if arg == "" {
-     continue
- }
```
*Symptom:* a bare `.` argument trims to an empty zone name, which is then processed as a
zone (a file named `""`, or an AXFR for the root).

**M-OPT-10** · `parseZoneArgs`, lines ~135–138 · O4 · **Silent**
```go
- if _, ok := zoneMap[arg]; !ok {
-     zones = append(zones, arg)
-     zoneMap[arg] = struct{}{}
- }
+ zones = append(zones, arg)
```
*Realistic because:* the map looks like premature optimisation to someone unaware of the
duplicate-transfer cost.
*Symptom:* a repeated zone argument is transferred and emitted more than once.

**M-OPT-11** · `parseZoneArgs`, line ~120 · O6 · **Silent**
```go
- server = normalizeAddrPort(after)
+ server = after
```
*Symptom:* the nameserver never receives a default port; every AXFR fails to dial.

**M-OPT-12** · `parseZoneArgs`, line ~122 · O4 · **Silent**
```go
  server = normalizeAddrPort(after)
- continue
```
*Symptom:* the `@server` argument falls through and is *also* recorded as a zone.

**M-OPT-13** · `parseZoneArgs`, line ~120 · O2 · **Silent**
```go
- server = normalizeAddrPort(after)
+ if server == "" {
+     server = normalizeAddrPort(after)
+ }
```
*Realistic because:* "don't overwrite what's already set" looks like a safety
improvement.
*Symptom:* with two `@` arguments the **first** wins instead of the last, silently
reversing precedence.

**M-OPT-14** · `parseFlags`, line ~71 · O1 · **Silent**
```go
- if len(zones) == 0 {
+ if len(zones) < 0 {          // never true
```
*Symptom:* an invocation with no usable zones proceeds instead of reporting the problem,
emitting a header-only hosts file.

**M-OPT-15** · `parseFlags`, line ~77 · O1 · **Silent**
```go
- if len(*cidrString) > 0 {
+ if len(*cidrString) >= 0 {   // always true
```
*Realistic because:* a boundary slip in a length check.
*Symptom:* with no `-cidr_list`, `strings.Split("", ",")` yields `[""]`, so CIDR
filtering switches **on** with one unparseable entry — the ranger is empty and every
record is filtered out. Output becomes header-only for all inputs.

**M-OPT-16** · `parseFlags`, line ~81 · O2 · **Silent**
```go
- if *resolverAddress != "" {
+ if *resolverAddress == "" {
```
*Symptom:* a supplied resolver address is never normalised, while an unset one becomes
`":53"` — pointing the resolver at localhost.

**M-OPT-17** · `parseFlags`, line ~85 · O5 · **Silent**
```go
- return zones, server, cidrList
+ return zones, "", cidrList
```
*Realistic because:* a merge artefact or a debugging stub left behind.
*Symptom:* remote mode silently degrades to local-file mode; zone names are treated as
filenames.

### Constants and defaults

**M-OPT-18** · line ~18 · O7 · **Silent**
```go
- dnsPort = "53"
+ dnsPort = "5353"
```
*Symptom:* the default nameserver port is wrong; transfers fail against real servers.

**M-OPT-19** · line ~19 · O7 · **Silent**
```go
- dnsPrefix = "@"
+ dnsPrefix = "#"
```
*Symptom:* `@server` is no longer recognised and is treated as a zone name.

**M-OPT-20** · line ~20 · O7 · **Silent**
```go
- cidrSeparator = ","
+ cidrSeparator = ";"
```
*Symptom:* a comma-separated CIDR list parses as a single malformed entry, so all
filtering silently drops everything.

**M-OPT-21** · line ~28 · O7 · **Silent**
```go
- greedyCNAME = flag.Bool("greedy_cname", true, ...)
+ greedyCNAME = flag.Bool("greedy_cname", false, ...)
```
*Symptom:* the default CNAME policy inverts; out-of-zone CNAMEs are dropped by default.

**M-OPT-22** · line ~29 · O7 · **Silent**
```go
- ignoreStar = flag.Bool("ignore_star", true, ...)
+ ignoreStar = flag.Bool("ignore_star", false, ...)
```
*Symptom:* wildcard records appear in output by default.

**M-OPT-23** · line ~24 · O7 · **Control**
```go
- defaultResolverTimeout = 10 * time.Second
+ defaultResolverTimeout = 11 * time.Second
```
*Symptom:* none observable in a correct run. A failure here means a test is asserting on
an incidental constant.

---

## 2. `zone.go`

### Zone-name plumbing

**M-ZON-01** · `processLocalZone`, line ~74 · O5 · **Silent**
```go
- processRecords(zoneName, doCIDR, ranger, hosts, zoneRecords)
+ processRecords(file, doCIDR, ranger, hosts, zoneRecords)
```
*Realistic because:* this was the historical behaviour — the parameter is named `zone`
and `file` was in scope under that name.
*Symptom:* `-strip_domain` and `-strip_unstrip` become no-ops for local zone files, and
the non-greedy CNAME test compares DNS names against a filesystem path.

**M-ZON-02** · `processLocalZone`, lines ~69–72 · O4 · **Silent**
```go
  zoneName := domain
- if zoneName == "" {
-     zoneName = zoneNameFromRecords(zoneRecords)
- }
```
*Symptom:* zone files carrying their own `$ORIGIN` (no `=domain` argument) lose domain
stripping.

**M-ZON-03** · `processLocalZone`, line ~45 · O1 · **Silent**
```go
- if len(t) == 2 {
+ if len(t) >= 2 {
```
*Realistic because:* "be liberal in what you accept."
*Symptom:* `file=a=b` is silently accepted using only the first two fields, with no
diagnostic about the ambiguity.

**M-ZON-04** · `processLocalZone`, lines ~46–47 · O5 · **Loud**
```go
- file = t[0]
- domain = dns.Fqdn(t[1])
+ file = t[1]
+ domain = dns.Fqdn(t[0])
```
*Symptom:* filename and domain swap; the read fails with a diagnostic naming the domain
as a path.

**M-ZON-05** · `processLocalZone`, line ~47 · O6 · **Silent**
```go
- domain = dns.Fqdn(t[1])
+ domain = t[1]
```
*Realistic because:* the `Fqdn` call looks cosmetic.
*Symptom:* the parser origin lacks its trailing dot, changing how relative names in the
zone file are qualified.

**M-ZON-06** · `processLocalZone`, line ~59 · O3 · **Silent**
```go
- if len(zoneRecords) == 0 && domain == "" {
+ if len(zoneRecords) == 0 || domain == "" {
```
*Symptom:* the "try `file=domain`" hint fires spuriously — whenever a domain was
supplied but the file is empty, and whenever records parsed fine but no domain was
given.

### Record processing

**M-ZON-07** · `processRecords`, line ~85 · O6 · **Silent**
```go
- zone = strings.ToLower(strings.TrimSuffix(zone, endingDot))
+ zone = strings.TrimSuffix(zone, endingDot)
```
*Symptom:* zone matching becomes case-sensitive while labels are still lowercased, so a
mixed-case zone argument silently disables stripping and the in-zone CNAME test.

**M-ZON-08** · `processRecords`, line ~85 · O11 · **Silent**
```go
- strings.ToLower(...)
+ strings.ToUpper(...)
```
*Symptom:* the zone never matches lowercased labels; stripping never applies.

**M-ZON-09** · `processRecords`, line ~85 · O6 · **Silent**
```go
- zone = strings.ToLower(strings.TrimSuffix(zone, endingDot))
+ zone = strings.ToLower(zone)
```
*Symptom:* a zone given with a trailing dot yields the suffix `..example.com.`, which
never matches; stripping silently stops.

**M-ZON-10** · `processRecords`, line ~182 · O9 · **Loud**
```go
- wg.Wait()
```
*Realistic because:* a refactor that moves waiting to the caller and forgets one site.
*Symptom:* the function returns while workers are still sending; `main` then closes the
channel underneath them. Expect `send on closed channel` panics and/or truncated output.
Run this one under `-race`.

**M-ZON-11** · `processRecords`, lines ~106 / ~125 · O3 · **Silent**
```go
- if *ignoreStar && strings.Contains(t.Hdr.Name, wildcard) {
+ if *ignoreStar || strings.Contains(t.Hdr.Name, wildcard) {
```
*Symptom:* with the default `-ignore_star=true`, **every** record is dropped; with it
false, wildcards are still dropped.

**M-ZON-12** · `processRecords`, lines ~106 / ~125 · O11 · **Silent**
```go
- strings.Contains(t.Hdr.Name, wildcard)
+ strings.HasPrefix(t.Hdr.Name, wildcard)
```
*Realistic because:* wildcards conventionally appear as a leading label, so this looks
like a tightening.
*Symptom:* `*.example.com` is still caught, but a wildcard in a non-leading position is
not — a partial, easily-missed regression.

**M-ZON-13** · `processRecords`, lines ~111 / ~130 · O2 · **Loud**
```go
- if !ok {
+ if ok {
```
*Symptom:* valid addresses are discarded and malformed ones are processed; output
collapses to near-empty or contains zero-value addresses.

**M-ZON-14** · `processRecords`, lines ~116 / ~135 / ~170 · O2 · **Silent**
```go
- if c, _ := ranger.Contains(ipAddr); !c {
+ if c, _ := ranger.Contains(ipAddr); c {
```
*Symptom:* the CIDR whitelist becomes a blacklist — exactly the records the user asked
for are the ones removed.

**M-ZON-15** · `processRecords`, lines ~115 / ~134 / ~169 · O3 · **Loud**
```go
- if doCIDR && ranger != nil {
+ if doCIDR || ranger != nil {
```
*Symptom:* the nil guard is defeated; a nil ranger is dereferenced.

**M-ZON-16** · `processRecords`, line ~140 · O5 · **Silent**
```go
- processHost(t.Hdr.Name, zone, ipAddr6, hosts)
+ processHost(zone, zone, ipAddr6, hosts)
```
*Realistic because:* a careless copy of the surrounding argument list.
*Symptom:* every AAAA record is labelled with the zone apex instead of its own name.

**M-ZON-17** · `processRecords`, line ~147 · O2 · **Silent**
```go
- if !*greedyCNAME {
+ if *greedyCNAME {
```
*Symptom:* the greedy/non-greedy CNAME policy inverts.

**M-ZON-18** · `processRecords`, line ~153 · O2 · **Silent**
```go
- if len(cnames) > 0 && !strings.HasSuffix(cnames[0], dns.Fqdn(zone)) {
+ if len(cnames) > 0 && strings.HasSuffix(cnames[0], dns.Fqdn(zone)) {
```
*Symptom:* in-zone CNAMEs are dropped and out-of-zone ones kept — the filter runs
backwards.

**M-ZON-19** · `processRecords`, line ~153 · O6 · **Silent**
```go
- !strings.HasSuffix(cnames[0], dns.Fqdn(zone))
+ !strings.HasSuffix(cnames[0], zone)
```
*Realistic because:* `zone` has already been normalised above, so the `Fqdn` looks
redundant.
*Symptom:* `LookupCNAME` returns trailing-dot names, so the suffix test mismatches on
the final dot and drops in-zone CNAMEs.

**M-ZON-20** · `processRecords`, line ~153 · O1 · **Loud**
```go
- if len(cnames) > 0 && ...
+ if len(cnames) >= 0 && ...
```
*Symptom:* index out of range on an empty result.

**M-ZON-21** · `processRecords`, line ~148 · O5 · **Silent**
```go
- cnames, err := lookup(ctx, t.Hdr.Name, dns.TypeCNAME, &r)
+ cnames, err := lookup(ctx, t.Hdr.Name, dns.TypeA, &r)
```
*Symptom:* the zone-membership test compares an IP address against the zone suffix,
which never matches, so non-greedy mode drops every CNAME.

**M-ZON-22** · `processRecords`, line ~166 · O10 · **Silent**
```go
- continue
+ break
```
*Symptom:* one unparseable address truncates the remaining addresses for that name.

**M-ZON-23** · `processRecords`, lines ~123–141 · O4 · **Silent**
```go
- case *dns.AAAA:
-     ...
```
(remove the whole case, letting AAAA fall to `default`)
*Realistic because:* a botched refactor merging the A and AAAA branches.
*Symptom:* all IPv6 records vanish from output with no diagnostic.

**M-ZON-24** · `zoneNameFromRecords`, line ~190 · O10 · **Silent**
```go
- return soa.Hdr.Name
+ return ""
```
*Symptom:* the SOA fallback yields nothing; `$ORIGIN`-only zone files lose stripping.

**M-ZON-25** · `zoneNameFromRecords`, lines ~188–192 · O5 · **Silent**
```go
- if soa, ok := rr.(*dns.SOA); ok {
-     return soa.Hdr.Name
- }
+ return rr.Header().Name
```
*Realistic because:* "the first record names the zone" is a plausible-sounding
shortcut.
*Symptom:* the first record's owner name is used as the zone, which is only correct when
that record happens to be the SOA.

### Parsing

**M-ZON-26** · `zoneParser`, lines ~211–223 · O8 · **Silent**
```go
  for rr, ok := zp.Next(); ok; rr, ok = zp.Next() {
+     if err := zp.Err(); err != nil {
+         continue
+     }
      records = append(records, rr)
  }
- if err := zp.Err(); err != nil { ...diagnostic... }
```
*Realistic because:* this is the historical shape and reads as "check as you go".
*Symptom:* `Next` reports `ok=false` at the first error, so the in-loop check can never
fire — a malformed line truncates the zone with no diagnostic at all.

**M-ZON-27** · `zoneParser`, line ~209 · O5 · **Silent**
```go
- zp := dns.NewZoneParser(bytes.NewReader(z), domain, "")
+ zp := dns.NewZoneParser(bytes.NewReader(z), "", "")
```
*Symptom:* the origin is dropped, so relative names in the file are not qualified.

**M-ZON-28** · `zoneParser`, line ~209 · O5 · **Silent**
```go
- dns.NewZoneParser(bytes.NewReader(z), domain, "")
+ dns.NewZoneParser(bytes.NewReader(z), "", domain)
```
*Symptom:* origin and file-label arguments swap; qualification breaks while diagnostics
gain a bogus filename.

**M-ZON-29** · `zoneParser`, line ~205 · O10 · **Control**
```go
- return records        // nil at this point
+ return []dns.RR{}
```
*Symptom:* none behavioural — callers only use `len`. A failure indicates an assertion on
nil-ness rather than emptiness.

### Address helpers

**M-ZON-30** · `unmapAddrFromSlice`, line ~235 · O6 · **Silent**
```go
- return ipAddr.Unmap(), true
+ return ipAddr, true
```
*Realistic because:* `Unmap` looks like a no-op for ordinary addresses.
*Symptom:* IPv4-mapped IPv6 addresses render as `::ffff:192.0.2.1` instead of
`192.0.2.1`, and the two forms stop deduplicating against each other.

**M-ZON-31** · `unmapAddrFromSlice`, lines ~231–233 · O10 · **Silent**
```go
- if !ok {
-     return ipAddr, false
- }
+ if !ok {
+     return ipAddr, true
+ }
```
*Symptom:* malformed address slices are accepted; zero-value addresses reach the output.

**M-ZON-32** · `unmapParseAddr`, line ~245 · O6 · **Silent**
```go
- return ipAddr.Unmap(), nil
+ return ipAddr, nil
```
*Symptom:* as M-ZON-30, on the CNAME-resolution path.

---

## 3. `hosts.go`

**M-HST-01** · `processHost`, line ~24 · O6 · **Silent**
```go
- label = strings.ToLower(label)
```
*Symptom:* hostname case from the wire leaks into output, and names differing only by
case stop merging onto one line.

**M-HST-02** · `processHost`, lines ~29–31 · O4 · **Silent**
```go
- if label == "" {
-     return
- }
```
*Symptom:* a root-owned record emits a hosts line with an address and no hostname.

**M-HST-03** · `processHost`, line ~33 · O3 · **Silent**
```go
- if *stripDomain || *stripUnstrip {
+ if *stripDomain && *stripUnstrip {
```
*Symptom:* each strip flag alone becomes a no-op; only passing both does anything.

**M-HST-04** · `processHost`, line ~35 · O2 · **Silent**
```go
- if labelStripped != "" {
+ if labelStripped == "" {
```
*Symptom:* the stripped form is emitted only when it is empty — i.e. never usefully;
stripping silently stops.

**M-HST-05** · `processHost`, line ~38 · O2 · **Silent**
```go
- if !*stripUnstrip {
+ if *stripUnstrip {
```
*Symptom:* the two flags swap meaning — `-strip_domain` emits both forms and
`-strip_unstrip` emits only the short one.

**M-HST-06** · `processHost`, line ~34 · O5 · **Silent**
```go
- labelStripped := strings.TrimSuffix(label, endingDot+zone)
+ labelStripped := strings.TrimSuffix(label, zone)
```
*Symptom:* the separating dot is left behind — `host.example.com` strips to `host.`
rather than `host`.

**M-HST-07** · `processHost`, line ~23 · O11 · **Control**
```go
- label = strings.TrimSuffix(label, endingDot)
+ label = strings.TrimRight(label, endingDot)
```
*Symptom:* differs only for names ending in multiple dots, which the DNS layer does not
produce. A failure suggests an assertion on an input that cannot occur in practice.

**M-HST-08** · `writeHostEntries`, lines ~52–58 · O4 · **Silent**
```go
  if _, ok := entries[ipAddr]; ok {
      entries[ipAddr][label] = struct{}{}
+     *keys = append(*keys, ipAddr)
  } else {
```
*Realistic because:* moving the append "so every entry is tracked".
*Symptom:* an address seen N times is appended to `keys` N times and printed on N
identical lines.

**M-HST-09** · `writeHostEntries`, lines ~52–58 · O4 · **Silent**
```go
- if _, ok := entries[ipAddr]; ok {
-     entries[ipAddr][label] = struct{}{}
- } else {
-     ...
- }
+ *keys = append(*keys, ipAddr)
+ entries[ipAddr] = make(map[string]struct{}, subMapSize)
+ entries[ipAddr][label] = struct{}{}
```
*Symptom:* each address keeps only its most recent label; multi-name lines collapse to
one name.

**M-HST-10** · `writeHostEntries`, line ~53 · O5 · **Silent**
```go
- entries[ipAddr][label] = struct{}{}
+ entries[ipAddr][ipAddr.String()] = struct{}{}
```
*Symptom:* hostnames are replaced by their own address text on merged lines.

---

## 4. `output.go`

**M-OUT-01** · lines ~32–34 · O6 · **Silent**
```go
- sort.Slice(keysAddr, func(i, j int) bool {
-     return keysAddr[i].Compare(keysAddr[j]) < 0
- })
```
*Realistic because:* "the map already gives us the keys, sorting is wasted work."
*Symptom:* address order follows Go's randomised map iteration — output differs between
otherwise identical runs, breaking reproducibility.

**M-OUT-02** · line ~33 · O1 · **Silent**
```go
- return keysAddr[i].Compare(keysAddr[j]) < 0
+ return keysAddr[i].Compare(keysAddr[j]) > 0
```
*Symptom:* addresses print in descending order.

**M-OUT-03** · line ~52 · O6 · **Silent**
```go
- sort.Strings(keysHost)
```
*Symptom:* hostnames within a line follow map order — the line content is stable but its
ordering is not, so output is non-reproducible.

**M-OUT-04** · line ~61 · O2 · **Silent**
```go
- if x != last {
+ if x == last {
```
*Symptom:* the separator moves to the wrong position — names run together and a trailing
space is appended.

**M-OUT-05** · line ~61 · O1 · **Control**
```go
- if x != last {
+ if x < last {
```
*Symptom:* none — `x` never exceeds `last`. A failure indicates coupling to the
expression rather than the output.

**M-OUT-06** · line ~46 · O4 · **Silent**
```go
- keysHost = keysHost[:0]
```
*Realistic because:* the reset looks redundant next to `sb.Reset()`.
*Symptom:* labels accumulate across lines; every line repeats all previously seen names.

**M-OUT-07** · line ~40 · O4 · **Silent**
```go
- sb.Reset()
```
*Symptom:* each line contains all preceding lines concatenated.

**M-OUT-08** · line ~44 · O5 · **Silent**
```go
- last = len(labelMap)
+ last = len(keysHost)
```
*Realistic because:* both look like "the number of names on this line".
*Symptom:* `keysHost` is not reset until two lines later, so `last` holds the **previous**
address's name count — zero on the first line. Separator placement then depends on the
preceding line's shape: names run together or gain a trailing space, varying per line.
Note this is data-dependent, so a single-address fixture may not expose it.

**M-OUT-09** · line ~42 · O7 · **Silent**
```go
- sb.WriteString("\t")
+ sb.WriteString(" ")
```
*Symptom:* address and names are space-separated rather than tab-separated. Still a valid
hosts file, but a formatting change that tooling may depend on.

**M-OUT-10** · line ~27 · O9 · **Loud**
```go
- defer w.Flush()
```
*Realistic because:* forgetting to flush a `bufio.Writer` is among the most common Go
mistakes.
*Symptom:* buffered output is discarded at exit — stdout is empty or truncated to a
partial buffer.

**M-OUT-11** · line ~30 · O4 · **Silent**
```go
- fmt.Fprintf(w, "# axfr2hosts generated list at %v\n", t)
```
*Symptom:* the provenance header disappears from generated files.

**M-OUT-12** · line ~38 · O5 · **Silent**
```go
- labelMap := results[ipAddr]
+ labelMap := results[keysAddr[0]]
```
*Symptom:* every line prints the first address's names.

---

## 5. `lookup.go`

**M-LKP-01** · lines ~61–62 · O11 · **Silent**
```go
- b := strconv.AppendUint(make([]byte, 0, len(s)+5), uint64(t), 10)
- key := string(append(b, s...))
+ key := s + strconv.FormatUint(uint64(t), 10)
```
*Realistic because:* the suffix form is more readable and looks equivalent.
*Symptom:* keys can collide across (name, type) pairs — e.g. name `a` type `12` and name
`a1` type `2` both produce `a12`, so one query returns the other's answer.

**M-LKP-02** · line ~62 · O4 · **Silent**
```go
- key := string(append(b, s...))
+ key := s
```
*Symptom:* the query type leaves the key entirely, so the CNAME and A lookups for one
name deduplicate into a single call and share a result.

**M-LKP-03** · line ~70 · O5 · **Silent**
```go
- if errors.Is(err, context.DeadlineExceeded) {
+ if errors.Is(err, context.Canceled) {
```
*Symptom:* after a timeout the singleflight key is never forgotten, so subsequent callers
join a dead in-flight call and inherit its failure.

**M-LKP-04** · line ~71 · O4 · **Silent**
```go
- lookupGroup.Forget(key)
```
*Symptom:* as M-LKP-03.

**M-LKP-05** · lines ~57–59 · O4 · **Loud**
```go
- if fn == nil {
-     return nil, fmt.Errorf("%w: %v", ErrUnsupportedType, dns.Type(t))
- }
```
*Symptom:* a nil closure reaches `singleflight.DoChan`, which panics — and re-panics in
every waiting caller, terminating the process.

**M-LKP-06** · line ~40 · O4 · **Silent**
```go
- case dns.TypeA, dns.TypeAAAA:
+ case dns.TypeA:
```
*Symptom:* AAAA lookups start returning `ErrUnsupportedType`.

**M-LKP-07** · lines ~36–38 · O11 · **Silent**
```go
- rr, err := r.LookupCNAME(ctx, s)
-
- return []string{rr}, err
+ return r.LookupHost(ctx, s)
```
*Symptom:* the CNAME branch returns addresses, so the zone-membership test compares an IP
against the zone suffix and never matches — non-greedy mode drops every CNAME.

**M-LKP-08** · lines ~33 / ~42 · O7 · **Silent**
```go
- ctx, cancel := context.WithTimeout(ctx, *resolverTimeout)
+ ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
```
*Symptom:* `-resolver_timeout` is silently ignored; slow resolvers fail.

**M-LKP-09** · lines ~34 / ~43 · O9 · **Silent**
```go
- defer cancel()
```
*Symptom:* contexts leak until their deadline. `go vet` may flag this — note whether it
is the vet stage or the test suite that catches it.

**M-LKP-10** · line ~38 · O10 · **Silent**
```go
- return []string{rr}, err
+ return []string{}, err
```
*Symptom:* the CNAME result is always empty, so the `len(cnames) > 0` guard short-circuits
and every CNAME is treated as in-zone.

**M-LKP-11** · line ~78 · O10 · **Silent**
```go
- return rrs, res.Err
+ return rrs, nil
```
*Symptom:* lookup failures are reported as success with an empty result, so callers
proceed on non-answers instead of skipping the record.

---

## 6. `axfr.go`

**M-AXF-01** · line ~26 · O6 · **Silent**
```go
- zone = dns.Fqdn(zone)
```
*Symptom:* the AXFR question name lacks its trailing dot; conforming servers answer
REFUSED and the zone comes back empty.

**M-AXF-02** · line ~41 · O6 · **Loud**
```go
- retry.Attempts(atLeastOne(*maxRetries)),
+ retry.Attempts(*maxRetries),
```
*Symptom:* `-max_retries=0` is read by the retry library as "retry until success",
hanging indefinitely against an unreachable server.

**M-AXF-03** · line ~41 · O7 · **Silent**
```go
- retry.Attempts(atLeastOne(*maxRetries)),
+ retry.Attempts(1),
```
*Symptom:* `-max_retries` is ignored; transient failures are no longer retried.

**M-AXF-04** · line ~66 · O10 · **Silent**
```go
- continue
+ break
```
*Symptom:* one recoverable envelope error truncates the remainder of the transfer,
yielding a partial zone that looks complete.

**M-AXF-05** · lines ~62–67 · O4 · **Silent**
```go
- if msg.Error != nil {
-     ...diagnostic...
-     continue
- }
```
*Symptom:* records from failed envelopes are appended without a diagnostic.

**M-AXF-06** · line ~69 · O5 · **Silent**
```go
- records = append(records, msg.RR...)
+ records = msg.RR
```
*Realistic because:* it reads as an assignment simplification.
*Symptom:* only the final envelope survives, so any zone large enough to be chunked
across messages is silently truncated.

**M-AXF-07** · line ~33 · O7 · **Loud**
```go
- tr.ReadTimeout = readTimeout
+ tr.ReadTimeout = 0
```
*Symptom:* no read deadline; a wedged server hangs the transfer indefinitely.

**M-AXF-08** · lines ~55–59 · O4 · **Loud**
```go
  if err != nil {
      fmt.Fprintf(os.Stderr, ...)
-     return records
  }
```
*Symptom:* execution falls through to `range c` on a nil channel, which blocks forever.

**M-AXF-09** · line ~30 · O11 · **Control**
```go
- m.SetAxfr(zone)
+ m.SetQuestion(zone, dns.TypeAXFR)
```
*Symptom:* likely none against a permissive server, since `SetAxfr` is close to this.
Worth running to see whether the suite over-specifies message construction.

**M-AXF-10** · line ~36 · O7 · **Control**
```go
- records := make([]dns.RR, 0, 1024)
+ records := make([]dns.RR, 0)
```
*Symptom:* none behavioural — capacity is a performance hint only.

---

## 7. `ranger.go`

**M-RNG-01** · line ~21 · O1 · **Silent**
```go
- if len(cidrList) > 0 {
+ if len(cidrList) >= 0 {
```
*Symptom:* CIDR filtering activates even with no list, and the empty ranger matches
nothing — all records are filtered out.

**M-RNG-02** · line ~30 · O10 · **Silent**
```go
- continue
+ break
```
*Symptom:* one malformed CIDR silently discards every subsequent entry in the list.

**M-RNG-03** · line ~28 · O4 · **Silent**
```go
- fmt.Fprintf(os.Stderr, "Error: problem parsing CIDR: %v\n", err)
```
*Symptom:* a typo'd CIDR is dropped with no warning, and the user sees an unexplained
reduction in output.

**M-RNG-04** · line ~33 · O4 · **Silent**
```go
- _ = ranger.Insert(n, struct{}{})
```
*Symptom:* the whitelist is never populated, so filtering removes everything.

**M-RNG-05** · line ~22 · O7 · **Silent**
```go
- doCIDR = true
+ doCIDR = false
```
*Symptom:* `-cidr_list` is accepted and parsed but never applied.

---

## 8. `main.go`

**M-MAIN-01** · lines ~113–115 · O8 · **Loud**
```go
- wgWrk.Wait()
  close(hostChan)
+ wgWrk.Wait()
```
*Symptom:* the channel closes while workers are still sending — `send on closed channel`
panic. Run under `-race`.

**M-MAIN-02** · line ~115 · O9 · **Loud**
```go
- wgMon.Wait()
```
*Symptom:* the map is read by `displayHostEntries` while the monitor goroutine may still
be writing it — a data race and truncated output. Run under `-race`.

**M-MAIN-03** · lines ~102–106 · O8 · **Silent**
```go
- semAXFR <- struct{}{}
  go func() {
      defer wgWrk.Done()
+     semAXFR <- struct{}{}
      defer func() { <-semAXFR }()
```
*Realistic because:* moving the acquire "inside where it is released" looks tidier.
*Symptom:* the semaphore no longer bounds anything — all zones transfer at once and
`-max_transfers` is ignored.

**M-MAIN-04** · line ~106 · O9 · **Loud**
```go
- defer func() { <-semAXFR }()
```
*Symptom:* slots are never returned; after `max_transfers` zones the loop blocks forever.

**M-MAIN-05** · line ~93 · O6 · **Loud**
```go
- semAXFR := make(chan struct{}, atLeastOne(*maxTransfers))
+ semAXFR := make(chan struct{}, *maxTransfers)
```
*Symptom:* `-max_transfers=0` gives a zero-capacity channel whose send blocks before any
receiver exists — the runtime aborts with `all goroutines are asleep - deadlock!`.

**M-MAIN-06** · line ~96 · O2 · **Silent**
```go
- if server == "" {
+ if server != "" {
```
*Symptom:* local and remote paths swap; zone names are opened as files and filenames are
sent to a nameserver.

**M-MAIN-07** · lines ~115–117 · O8 · **Loud**
```go
- wgMon.Wait()
- displayHostEntries(keys, entries)
+ displayHostEntries(keys, entries)
+ wgMon.Wait()
```
*Symptom:* output is rendered from a partially populated map; results vary run to run.

**M-MAIN-08** · line ~101 · O9 · **Loud**
```go
- wgWrk.Add(1)
```
*Symptom:* the wait group does not track remote workers, so `main` proceeds to close the
channel and print while transfers are still running.

**M-MAIN-09** · line ~81 · O7 · **Control**
```go
- hostChan := make(chan HostEntry, hostChanSize)
+ hostChan := make(chan HostEntry)
```
*Symptom:* none behavioural — an unbuffered channel is slower but correct. A failure
indicates a test coupled to buffering.

**M-MAIN-10** · line ~78 · O4 · **Control**
```go
- _ = setNofile()
```
*Symptom:* only observable under very high concurrency with many open sockets; unlikely
to change ordinary output.

---

## 9. `init.go` and `rlimit_unix.go`

**M-INI-01** · `init.go`, lines ~15–18 · O6 · **Silent**
```go
- GitTag = strings.TrimSpace(GitTag)
- GitCommit = strings.TrimSpace(GitCommit)
- GitDirty = strings.TrimSpace(GitDirty)
- BuildTime = strings.TrimSpace(BuildTime)
```
*Realistic because:* link-time values usually look clean already.
*Symptom:* `-version` output carries stray whitespace or embedded newlines from the
build-time injection.

**M-RLM-01** · `rlimit_unix.go`, line ~27 · O2 · **Silent**
```go
- if runtime.GOOS == "darwin" {
+ if runtime.GOOS != "darwin" {
```
*Symptom:* the platform limits swap; macOS requests a value above its `setrlimit(2)`
ceiling and the call fails.

**M-RLM-02** · `rlimit_unix.go`, lines ~14–15 · O7 · **Silent**
```go
- darwinMagic   = 24576
+ darwinMagic   = 245760
```
*Symptom:* the raise fails on macOS; the process keeps the default descriptor limit.

**M-RLM-03** · `rlimit_unix.go`, lines ~28 / ~34 · O10 · **Silent**
```go
- return syscall.Setrlimit(...)
+ _ = syscall.Setrlimit(...)
+ return nil
```
*Symptom:* failures are reported as success. Note that `main` discards this error
anyway — worth recording whether anything observes it at all.

---

## Suggested ordering

The catalogue is large. If working through it incrementally, this ordering front-loads
the most informative cases:

1. **Silent output corruption** — M-OUT-01, M-OUT-03, M-OUT-06, M-HST-08, M-HST-09,
   M-AXF-06, M-ZON-23. These produce a plausible-looking hosts file that is wrong.
2. **Inverted filters** — M-ZON-14, M-ZON-18, M-HST-04, M-HST-05, M-RNG-05. Semantics
   reverse while everything still "works".
3. **Concurrency** — M-ZON-10, M-MAIN-01, M-MAIN-02, M-MAIN-04, M-MAIN-08. Run each
   under `-race` *and* without, and record whether plain `go test` alone suffices.
4. **Normalisation removal** — M-ZON-07, M-ZON-30, M-HST-01, M-AXF-01, M-OPT-04. These
   fail only for specific input shapes (mixed case, IPv6, mapped addresses).
5. **Controls** — M-OPT-23, M-ZON-29, M-HST-07, M-OUT-05, M-AXF-09, M-AXF-10,
   M-MAIN-09, M-MAIN-10. Any failure here is a brittleness finding.
6. Everything else.

## Interpreting the results

- **Undetected Silent mutations** are the headline result — each is a class of bug the
  suite would not stop from shipping.
- **Undetected Loud mutations** are more serious still: if a panic, hang, or deadlock
  goes unnoticed, the suite is not exercising that path at all.
- **Detected Controls** indicate over-specification — tests asserting on incidental
  detail that will generate false failures during ordinary refactoring.
- **Detected-but-undiagnostic** cases matter for maintenance: a test that fails with
  `want 3, got 2` and no indication of why costs far more to debug than one that names
  the broken invariant.

A useful summary at the end: detection rate for Silent, detection rate for Loud, and
false-positive rate on Controls. The first two should be high; the third should be zero.
