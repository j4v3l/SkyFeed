# 0001: Go runtime and dependency policy

- Status: Accepted
- Date: 2026-08-22

## Decision

Use module `github.com/j4v3l/SkyFeed` under Apache-2.0 and pin Go 1.27.0 in
the patch-level `go` directive. A matching `toolchain` directive is omitted
because Go 1.27 treats it as redundant and removes it during module updates.
Pin direct dependencies before use, retain `go.sum`, and review licenses and
release notes during upgrades.

Initial pins are disgo v0.19.6, modernc.org/sqlite v1.57.0 (with
modernc.org/libc v1.74.4), and golang.org/x/sync v0.22.0. Staticcheck must use
the Go 1.27-capable v0.8.0-rc.1 until a compatible stable release supersedes
it; govulncheck is pinned to v1.7.0.

## Consequences

Go provides static builds, bounded concurrency primitives, profiling, and low
runtime overhead. Dependencies are added only at material boundaries. Phase 0
records compile/runtime spikes before application code depends on them.

Go 1.26, unpinned tools, and adding abstraction libraries for standard-library
features were rejected because they conflict with the specification or reduce
reproducibility without material value.
