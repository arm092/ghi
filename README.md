# Ghi – Go, Hierarchy, Interfaces

[![Sponsor on GitHub](https://img.shields.io/badge/Sponsor-30363D?logo=githubsponsors&logoColor=EA4AAA)](https://github.com/sponsors/arm092)

Ghi is a statically typed language for backend applications. It combines Go-like syntax and the Go runtime with classes, inheritance, constructors, typed exceptions, nullable types and namespaces.

Ghi compiles your project to Go, invokes the Go toolchain, and produces a native executable. Applications use Go's garbage collector, goroutines, channels and library ecosystem. There is no interpreter to install on the deployment machine.

**Current release:** [Ghi v0.2.6](https://github.com/arm092/ghi/releases/tag/v0.2.6), bundled with independently versioned [Mojave v0.1.0](https://github.com/arm092/mojave/releases/tag/v0.1.0). **IDE:** [Ghi for GoLand v0.1.3](https://github.com/arm092/ghi-goland/releases/tag/v0.1.3).

Ghi is an experimental, pre-1.0 language. Syntax and package contracts may change. This README documents the implemented language; the [examples](examples) provide runnable projects.

The language tools and GoLand plugin are released under the [MIT License](LICENSE).

## Contents

- [Installation](#installation)
- [Quick start](#quick-start)
- [Development watch mode](#development-watch-mode)
- [How compilation works](#how-compilation-works)
- [Performance](#performance)
- [Files, namespaces and imports](#files-namespaces-and-imports)
- [Types and collections](#types-and-collections)
- [Enums](#enums)
- [Classes and inheritance](#classes-and-inheritance)
- [Interfaces and generics](#interfaces-and-generics)
- [Nullable values](#nullable-values)
- [Functions and closures](#functions-and-closures)
- [Exceptions and native Go errors](#exceptions-and-native-go-errors)
- [Control flow and match](#control-flow-and-match)
- [Concurrency](#concurrency)
- [Go interoperability](#go-interoperability)
- [Mojave packages](#mojave-packages)
- [Testing and formatting](#testing-and-formatting)
- [GoLand support](#goland-support)
- [Examples and current boundaries](#examples-and-current-boundaries)

## Installation

Download the archive for your operating system and CPU from the [compiler release](https://github.com/arm092/ghi/releases/tag/v0.2.6).

| Platform | CPU | Archive suffix |
| --- | --- | --- |
| Windows | Intel/AMD 64-bit | `windows_amd64.zip` |
| Windows | ARM64 | `windows_arm64.zip` |
| macOS | Intel | `darwin_amd64.tar.gz` |
| macOS | Apple silicon | `darwin_arm64.tar.gz` |

Extract the complete archive, keeping both executables, their checksum files and the installer together. Outer archive hashes are in `checksums.txt` on the release page.

### Windows

Download and run [Ghi Setup](https://github.com/arm092/ghi/releases/download/v0.2.6/ghi_v0.2.6_windows_setup.exe). The wizard selects the native x64 or ARM64 binaries, installs Ghi and Mojave, prepares Go and adds the commands to your user PATH. No administrator access is required. Open a new terminal after installation.

The default directory is `%LOCALAPPDATA%\Ghi`. Uninstall through **Settings → Apps → Installed apps → Ghi and Mojave**. The uninstaller removes its own PATH entry and installed files; projects and downloaded Go caches are retained. The installer is currently unsigned. Its SHA-256 file is available alongside the executable in the release.

For portable archive installation, extract the ZIP and run in PowerShell:

```powershell
.\install.ps1
```

The portable script defaults to `%LOCALAPPDATA%\Ghi\bin`. Optional arguments: `-InstallDir <directory>` and `-NoPath`.

### macOS

With Homebrew:

```sh
brew tap arm092/ghi https://github.com/arm092/ghi
brew install arm092/ghi/ghi
```

The formula builds Ghi and Mojave from the verified release source and installs Go as a dependency. Update with `brew update && brew upgrade ghi`; uninstall with `brew uninstall ghi`. Native Homebrew verification on macOS is pending.

For v0.2.6 on macOS, use the portable archive or Homebrew. A native v0.2.6 `.pkg` has not been built; native macOS and Homebrew checks are deferred. The previous [v0.2.1 universal macOS installer (.pkg)](https://github.com/arm092/ghi/releases/download/v0.2.1/ghi_v0.2.1_macos_universal.pkg) remains available and does not include enums or editor-buffer checks. It contains Intel and Apple silicon binaries for Ghi v0.2.1 and Mojave v0.1.0 and prepares Go for the signed-in user before installation. Installation, version commands, managed Go 1.26.8 setup, project creation and compilation/execution were verified on an Apple silicon Mac. The package is unsigned and has not been notarized by Apple. Its `.sha256` file is available in the release. Homebrew and `.pkg` are alternative installation methods; the package refuses to overwrite another installation. The [installer build kit](https://github.com/arm092/ghi/releases/download/v0.2.6/ghi_v0.2.6_macos_installer_kit.zip) and `scripts/package-macos.sh` provide the build recipe.

For portable archive installation, extract the macOS archive and run:

```sh
sh install.sh
```

The default directory is `~/.local/bin`. The installer configures zsh or bash startup files; open a new terminal afterwards. Set `GHI_INSTALL_DIR` to choose another directory or `GHI_NO_PATH=1` to manage PATH yourself.

### Automatic Go setup

The archive scripts and Windows wizard run `ghi setup`. A compatible Go installation is reused. If none is available, Ghi downloads an official Go distribution, verifies its SHA-256 checksum and installs it in a per-user cache. Initial setup may need internet access. Homebrew installs Go as a dependency. The macOS `.pkg` requires administrator authentication for system-wide Ghi installation; the Windows wizard and archive scripts do not.

```sh
ghi version
ghi setup
ghi setup --managed
mojave help
```

`--managed` selects or installs a managed toolchain independently of system Go. Compiled applications do not require Ghi or Go to be installed on the target machine.

The full release test suite was run locally on Windows. The native v0.2.1 macOS package was built on a Mac. Installation, `ghi -v`, `mojave -v`, `ghi setup`, `ghi init` and `ghi run .` were verified on Apple silicon; the generated project printed `Hello from Ghi!`. Native Intel macOS execution and Homebrew installation remain unverified. GitHub Actions is disabled for this repository.

## Quick start

Create a project:

```sh
ghi init my-app
cd my-app
ghi run
```

`ghi init` without a directory initializes the current directory, including an existing Git checkout. It creates `main.ghi`, `mojave.json`, a separate `tests/` directory and a `.gitignore` for generated files. Existing `main.ghi` or `mojave.json` files cause an error before anything is written; an existing `.gitignore` is preserved. It does not initialize Git or download dependencies.

Both tools accept `version`, `--version`, `-v` and `-V`. Ghi and Mojave have independent versions; a Ghi release bundles a specific Mojave version. The `-v` flag after `ghi test` still means verbose test output.

The executable entry point is a `main.ghi` file such as:

```ghi
namespace main

import fmt "go:fmt"

func main() {
	fmt.Println("Hello from Ghi")
}
```

Run these commands inside that directory:

```sh
ghi check .
ghi run .
ghi build -o hello .
```

Use `-o hello.exe` on Windows. Run the compiled executable directly: `./hello` on macOS or `.\hello.exe` on Windows. `ghi run` accepts a source project, not an already compiled binary. Pass application arguments with `ghi run . -- argument1 argument2`.

## Development watch mode

```sh
ghi watch .
ghi watch --debug . -- application-argument
```

`watch` polls project contents and debounces saves, builds a fresh executable, then replaces the running development process only after compilation succeeds. A failed build prints diagnostics and leaves the previous service running. Saves during compilation schedule another build; an obsolete build is not started. A program that exits waits for the next source change instead of restarting in a loop. Arguments after `--` are forwarded, and the child runs in the project directory with inherited environment and terminal streams.

Watched extensions are `.ghi`, `.go`, `.json`, `.lock`, `.sql`, `.mod` and `.sum`. Installed package files under `.ghi/packages` are included. Other hidden directories, `bin`, `vendor`, `node_modules` and each project or package root's test directory are excluded (nested production directories named tests remain watched); generated caches, database files and executables do not trigger rebuilds. Changes outside the project tree, including external local Go replacements, require a project edit or a new watch session. Dependency changes still require `mojave install` or `mojave update` as appropriate.

Ctrl+C cancels compilation and stops the service. Unix uses a process group, with a one-second termination grace period before forced termination; Windows uses a job object and terminates the process tree on replacement or shutdown. This is a development workflow, not a production process supervisor. Watch mode does not run tests automatically.

### Test watching (v0.2.6)

Available since v0.2.6:

```sh
ghi test --watch .
ghi test --watch -run '^TestRepository' -v -timeout 30s .
```

Test watching runs immediately, includes production sources and the project's root `tests/` directory, and reruns after changes. Runs are serialized; edits during a run schedule one subsequent run after saves settle. Failed tests and compiler errors keep the watcher running. A result from a run with observed source changes is marked as outdated instead of reported as a current pass. Installed dependencies retain their own test exclusions. The watched file extensions and output exclusions are the same as ordinary watch mode. Ctrl+C cancels the active test process tree and exits; invalid filters, timeouts and project paths fail at startup.

### Source diagnostics (v0.2.6)

The CLI adds the original source line and a caret to located errors from `check`, `build`, `run`, `test` and watch commands. Tabs are expanded for display, byte-based source columns are translated across Unicode text, and argument/type mismatch errors include an expected/received explanation when available. Diagnostics retain the original `file:line[:column]: message` header and multiline `have`/`want` details. Errors without an available project source location retain their original text. Editor checks through `check --stdin --filename` keep the existing plain output and never display stale on-disk source.

## How compilation works

1. Ghi loads `.ghi` sources, namespaces and installed Mojave dependencies.
2. It parses and validates Ghi constructs, then lowers them into Go declarations and expressions.
3. Type checking validates assignments, generic constraints, method signatures and Ghi-specific rules such as visibility and nonnullable initialization.
4. The compiler prepares a Go module and invokes the selected Go toolchain. The development compiler can reuse generated files in `.ghi/build`; debug, test and editor-overlay builds use temporary workspaces.
5. The result is an executable for the target platform. Debug builds retain mappings to the original Ghi files.

Generic types remain typed Go generics. Classes use generated interfaces and storage, preserving dynamic dispatch. Exceptions use generated runtime support. These transformations can introduce overhead; using the Go backend does not guarantee that every Ghi program performs identically to hand-written Go. Generated source and the generated object ABI are implementation details.

## Performance

Ghi uses the Go compiler and runtime, but generated abstractions can add overhead. Performance depends on the workload; compiling to Go does not guarantee identical execution time.

Since v0.2.3, the compiler specializes method and constructor receivers for classes with no descendants in the compiled project. Direct `this` member access can then use a concrete Go pointer, allowing Go to inline calls. Public class types, alias types and virtual dispatch retain their existing semantics. Methods that rebind `this` or take its address keep the original implementation. Debug builds disable this optimization. This optimization is available starting with v0.2.3.

Since v0.2.3, the compiler generates concrete copies of small method bodies for inheritance hierarchies within one namespace. Version v0.2.4 extends this to cross-namespace and generic inheritance. Original shared bodies remain available for `parent` calls and dynamic receivers. Constructors in inheritance hierarchies are unchanged.

The opt-in comparison suite lives in `tests/performance`. It builds Ghi and Go fixtures with the same Go toolchain, checks workload results, and measures arithmetic, fields, methods, virtual dispatch, object allocation, nullable values, error handling and an in-process HTTP handler. It reports time, bytes and allocations per operation, using five samples with alternating execution order. Build time is outside the runtime measurements.

```powershell
$env:GHI_PERF = "1"
go test ./tests/performance -run TestComparison -v -count=1
```

On macOS or Linux, use `GHI_PERF=1 go test ./tests/performance -run TestComparison -v -count=1`.

The HTTP fixture exercises `httptest` and an empty 204 response, without a network or database. The error fixtures compare idiomatic Go error returns with Ghi exceptions: Ghi also captures a stack trace, so these workloads provide different diagnostics. Their ratio is not a general language speed comparison. Nanosecond-scale results are sensitive to the CPU, compiler and background load. The older `benchmarks/run.py` harness separately measures aggregate workloads and controlled cold/warm Go build caches.

Snapshot: Windows amd64, Intel Core i9-13900HX, Go 1.26.3, September 26, 2026. Medians of five 150 ms samples; lower is better. The before run disables receiver specialization with the same workloads. These are local measurements, not cross-platform guarantees. [Raw samples](tests/performance/results/windows-amd64-go1.26.3.csv).

| Workload | Ghi before (ns/op) | Ghi after (ns/op) | Go after (ns/op) |
| --- | ---: | ---: | ---: |
| Arithmetic | 1.085 | 1.076 | 1.187 |
| Fields | 1.639 | 1.544 | 1.663 |
| Method | 3.065 | 0.494 | 0.514 |
| Virtual dispatch | 4.224 | 2.748 | 1.482 |
| Escaping object creation | 10.09 | 9.51 | 9.33 |
| Nullable primitive | 0.481 | 0.468 | 0.484 |
| Error handling, success path | 6.655 | 6.600 | 1.719 |
| Error handling, failure path | 3455 | 3119 | 16.36 |
| In-process HTTP handler | 51.71 | 44.59 | 44.86 |

The method fixture improves about 6.2 times and virtual dispatch about 1.5 times. Small differences in the other fixtures should not be attributed to this optimization. Escaping object creation allocates 8 bytes once per operation in both languages. The failure fixture allocates 1,040 bytes across eight allocations in Ghi, versus 16 bytes in one allocation for a Go error without a stack trace.

A second comparison on the same machine/toolchain measures inheritance specialization against the preceding leaf-only optimization. This run adds an inherited-method fixture; it is separate from the first snapshot above. [Inheritance samples](tests/performance/results/inheritance-windows-amd64-go1.26.3.csv).

| Workload | Ghi leaf-only (ns/op) | Ghi inheritance specialization (ns/op) | Go (ns/op) |
| --- | ---: | ---: | ---: |
| Inherited method | 3.261 | 0.497 | 0.494 |
| Virtual dispatch | 2.833 | 1.370 | 1.383 |

The inherited-method fixture improves about 6.6 times, and virtual dispatch about 2.1 times. Both have zero allocations per operation. The Ghi benchmark binary grows from 5,921,792 to 5,922,816 bytes (1 KiB, about 0.02%); the Go binary is 5,882,368 bytes in both runs. These sizes include default Go debug information. Binary growth in larger class hierarchies can differ. Exception allocation counts remain unchanged at eight in the failure fixture; this pass does not optimize exception handling.

Version v0.2.4 also specializes generic inheritance, including concrete descendants of generic bases, generic descendants and nested ancestor arguments. The following Windows amd64 / Go 1.26.3 comparison uses the same fixtures before and after this optimization, with five alternating 150 ms samples per language (September 27, 2026):

| Generic inherited method | Ghi before (ns/op) | Ghi after (ns/op) | Go after (ns/op) |
| --- | ---: | ---: | ---: |
| Concrete descendant of generic base | 3.838 | 1.140 | 0.415 |
| Generic descendant, instantiated with `int` | 4.243 | 1.057 | 0.416 |

These fixtures improve about 3.4 and 4.0 times respectively, with zero allocations in both languages. Go remains faster in these fixtures; generic specialization does not guarantee elimination of all dispatch/dictionary overhead. The Ghi benchmark binary grows by 512 bytes, from 5,956,608 to 5,957,120; the Go binary stays at 5,886,464 bytes. Results include Go debug information and are workload-specific. [Raw samples](tests/performance/results/generic-inheritance-windows-amd64-go1.26.3.csv).

Since v0.2.4, the compiler specializes small inherited methods across namespaces, including generic ancestors and descendants. Imports and named types retain their original bindings; virtual calls, `parent`, private class members, captured receivers and original exception source locations keep their semantics. Copies are limited to 128 AST nodes per method body. Rebinding/address-taking of `this`, inaccessible namespace declarations and conflicting built-in names retain the shared implementation. Debug builds disable receiver specialization. Generic substitutions use resolved type parameters and generated aliases to preserve local bindings.

A cross-namespace fixture uses a base class in a separate package and calls its inherited method on a concrete descendant. On the same Windows amd64 machine with Go 1.26.3 (September 27, 2026), medians of five 150 ms samples are:

| Workload | Ghi before (ns/op) | Ghi after (ns/op) | Go after (ns/op) |
| --- | ---: | ---: | ---: |
| Cross-namespace inherited method | 2.638 | 0.461 | 0.456 |

This specific call improves about 5.7 times, with zero bytes and zero allocations per operation in all variants. The Ghi benchmark binary changes from 5,947,392 to 5,946,368 bytes (1 KiB smaller); the Go binary remains 5,885,440 bytes. Sizes include Go debug information. Before and after runs use identical fixtures and the same Go toolchain; each alternates Ghi and Go process order. These microbenchmarks demonstrate removal of dispatch overhead for an eligible method, not an application-wide speedup. Other workloads and larger hierarchies can have different results and binary growth. [Raw samples](tests/performance/results/cross-namespace-windows-amd64-go1.26.3.csv).

The runtime caches immutable frame descriptions for up to 128 distinct complete PC sequences shorter than 64 entries, replacing old entries in insertion order. Addresses are captured at every throw; stack traces remain immediately available. Every exception receives fresh mutable `StackFrame` objects and its own slice. Repeated throws of an exception keep its existing nonempty trace, including user-supplied frames. Cache misses still require symbol resolution, and the cache retains a bounded amount of metadata for the process lifetime.

The first exception-focused comparison against the inheritance-optimized compiler on the same Windows machine and Go 1.26.3 gives the following medians. In that version, the deep fixture added 96 recursive calls and bypassed the cache. [Exception samples](tests/performance/results/exceptions-windows-amd64-go1.26.3.csv).

| Ghi workload | Before (ns/op) | After (ns/op) | Before / after bytes | Before / after allocations |
| --- | ---: | ---: | ---: | ---: |
| Successful operation inside `try` | 6.364 | 6.466 | 0 / 0 | 0 / 0 |
| Repeated exception, cached stack | 2816 | 1149 | 1040 / 272 | 8 / 6 |
| Deep exception, uncached stack | 22091 | 22205 | 11072 / 10608 | 111 / 112 |

The repeated-exception fixture is about 2.5 times faster and allocates about 74% fewer bytes. The deep path remains approximately the same speed, with one additional temporary allocation and fewer bytes overall. These results do not predict first-throw or cache-miss latency. The benchmark binary grows by 8 KiB. Ordinary Go errors still do less work: the Go failure fixture does not capture a stack. Fatal reporting also reuses an exception's existing trace instead of capturing a redundant second stack.

Since v0.2.3, the runtime additionally caches up to 16 complete deeper PC sequences shorter than 256 entries. Longer stacks bypass both caches and grow their capture buffer until the whole stack fits. Cached traces allocate their mutable frame objects together in one backing array, reducing allocation count without sharing objects between exceptions. Retaining one frame keeps that trace's backing array alive; the extra cache also retains bounded metadata. A cache hit still walks the current Go stack and unwinds the exception through `panic`/`recover`.

A subsequent comparison against the first stack-cache implementation, using the same machine, Go 1.26.3 and five samples per fixture, measured the following medians. The before and after runs were performed without the test suite running alongside them. [Frame allocation and deep-cache samples](tests/performance/results/exception-frames-windows-amd64-go1.26.3.csv).

| Ghi workload | Before (ns/op) | After (ns/op) | Before / after bytes | Before / after allocations |
| --- | ---: | ---: | ---: | ---: |
| Successful operation inside `try` | 6.337 | 6.135 | 0 / 0 | 0 / 0 |
| Repeated shallow exception | 1086 | 1008 | 272 / 256 | 6 / 4 |
| Exception with 96 recursive calls | 21509 | 6670 | 10608 / 5968 | 112 / 4 |
| Exception with 300 recursive calls, uncached | 63155 | 57641 | 40880 / 37808 | 320 / 318 |

The 96-call fixture is about 3.2 times faster once its deeper stack is cached. The shallow fixture improves modestly in this run (about 7%); small timing differences should not be treated as a universal speedup. The 300-call fixture remains uncached and uses fewer capture-buffer allocations. The Ghi benchmark binary grows by 10 KiB, from 5,933,056 to 5,943,296 bytes; the Go binary remains 5,883,392 bytes. These measurements do not cover cold cache misses, application throughput or contention under load. Full trace capture remains more expensive than returning a Go error without a trace.

A further v0.2.3 optimization captures up to 256 PCs in one traversal, then selects the existing shallow or deep cache. Previously, a cacheable deep trace required a 64-PC traversal followed by another traversal into the larger buffer. Traces of 256 PCs or more still grow their capture buffer and remain complete. The larger initial buffer is stack-local; the cache keys and public trace ownership are unchanged.

A comparison against the preceding implementation on the same Windows machine and Go 1.26.3, with five samples per fixture, produced these medians. [Single-pass capture samples](tests/performance/results/exception-single-pass-windows-amd64-go1.26.3.csv).

| Ghi exception workload | Before (ns/op) | After (ns/op) | Bytes / allocations, unchanged |
| --- | ---: | ---: | ---: |
| Repeated shallow exception | 986.4 | 1004 | 256 / 4 |
| 96 recursive calls, cached | 6611 | 5115 | 5968 / 4 |
| 300 recursive calls, uncached | 57380 | 55135 | 37808 / 318 |

The cached 96-call fixture takes about 23% less time. The shallow fixture remains around one microsecond, with a roughly 2% increase in this run; small differences are sensitive to measurement noise. Both benchmark binary sizes are unchanged. These results still describe warmed microbenchmarks, not cold-cache latency or application throughput.

### Build performance

Since v0.2.3, the compiler loads Go export metadata in one batched `go list` request for the project's external imports. Previously it launched a separate request whenever a needed package was not in the per-build export map. The map is recreated for every compilation, so source and dependency changes are still checked by Go. Failed batches fall back to individual imports to preserve source-located diagnostics. Dependency downloads, checksum verification and language validation remain enabled.

On the DDD API example, Windows amd64 and Go 1.26.3, the following wall times were observed. Each row is one before/after observation, not a statistical median. Go's build and module caches were already populated; "initial" means the first invocation on the copied benchmark project, not a cold toolchain. The edited case changes an application log string in `main.ghi`. [Build samples](tests/performance/results/build-imports-windows-amd64-go1.26.3.csv).

| DDD API build | Before | After |
| --- | ---: | ---: |
| Initial invocation, populated Go cache | 6.77 s | 3.93 s |
| Repeated, unchanged sources | 6.66 s | 4.05 s |
| After editing one source file | 6.57 s | 3.92 s |

The repeated build takes about 39% less time in this run. Its export-loading subprocesses decrease from 14 to 1 (3.01 s to 0.31 s). Total Ghi lowering, which includes export loading and type checks, decreases from 3.39 s to 0.62 s. Dependency preparation remains about 1.8 s and Go compilation/linking about 1.5 s. Stage timings were collected with temporary instrumentation; the released CLI output is unchanged. Results depend on filesystem caches, dependencies and the Go toolchain. There is no persistent Ghi AST or executable cache in this change.

A separate run gave each compiler a fresh, independent `GOCACHE`, while keeping the module and OS caches populated. The first build took 40.68 s before and 16.53 s after; the immediate warm repeats took 6.25 s and 3.96 s. Batching lets Go schedule the complete set of imported packages together. These are single observations in before/after order, not a promise of a fixed cold-build speedup or a fresh-machine installation benchmark. [Isolated Go-cache samples](tests/performance/results/build-imports-cold-windows-amd64-go1.26.3.csv).

#### Incremental builds (v0.2.4)

Normal `ghi build`, `ghi run` and `ghi check` reuse validated generated Go code in `.ghi/build/work`. The cache key includes source contents and paths, installed Ghi package sources, manifests and lockfiles, the compiler executable, selected Go toolchain, resolved Go settings and external Go export artifacts. Local Go `replace` dependencies are checked by Go too. Source discovery, Mojave validation, dependency downloads and checksum verification still run. Generated files are hashed before reuse; missing or damaged cache data causes regeneration.

Unchanged inputs skip Ghi lowering and semantic checking. In the current source checkout, edits invalidate the changed namespace and its transitive consumers. Unaffected namespaces reuse their validated generated bodies; their declarations are still checked to reconstruct type information. Receiver specialization requires the affected inheritance component to be checked together, and changes to the class inheritance graph invalidate all namespace certificates conservatively. Compiler, toolchain, environment, dependency and manifest changes also invalidate certificates. Namespace-level semantic reuse is included in v0.2.5.

Only changed generated files are written to the stable workspace, allowing Go to reuse unaffected compiled packages. This is not a persistent per-file Ghi AST cache: source discovery, parsing and structural validation still run. The compiler also seeds its temporary output from the previous executable, allowing Go to skip unnecessary linking after checking build IDs; failed builds preserve the previous output. Embedders can supply `compiler.Options.Stats` to observe checked and reused namespaces; runtime implementation namespaces are excluded from those counters.

Debug builds, test runs and unsaved editor overlays use fresh temporary workspaces. Concurrent builds use an OS lock; a busy, read-only or unavailable cache falls back to temporary compilation. The lock is released by the OS when the process exits, including after a crash. To clear generated build data, remove `.ghi/build` while no compilation is running; installed packages under `.ghi/packages` are separate. Dependency checksum verification runs concurrently with Ghi analysis; both must succeed before generated output or an executable can be accepted.

On the DDD API example, Windows amd64, Core i9-13900HX and Go 1.26.3 (September 27, 2026), five measured samples after one warm-up pair gave these median wall times. Each pair builds unchanged sources, then changes the offset expression in `application/users/service.ghi` and builds again. The next unchanged build uses that edited source. Both compiler versions use the same copied project, output path and populated Go/module caches; phases run sequentially with no test suite running alongside them.

| DDD API build | Before | After |
| --- | ---: | ---: |
| Unchanged sources | 3.987 s | 2.797 s |
| After editing one service | 3.822 s | 3.859 s |

The unchanged build takes about 30% less time. The edited case is effectively unchanged within the observed variation; this implementation does not claim faster per-file Ghi semantic analysis. Dependency checksum verification alone still takes about 1.5 seconds on this project. Cold caches, other dependency sets and other platforms can differ. Sample zero is the warm-up pair and is excluded from these medians. [Raw samples](tests/performance/results/incremental-build-windows-amd64-go1.26.3.csv).

The final v0.2.4 pipeline overlaps checksum verification with Ghi analysis, while still requiring successful verification before accepting cached or newly generated output. Repeating the same benchmark protocol yields:

| DDD API build | Before caching/overlap | Final v0.2.4 pipeline |
| --- | ---: | ---: |
| Unchanged sources | 3.987 s | 2.095 s |
| After editing one service | 3.822 s | 2.934 s |

These v0.2.4 medians are about 47% and 23% lower respectively. The earlier five-sample baseline is reused; Go/module caches remain warm, and there are no parallel tests during measurement. The changed-file speedup comes from overlapping independent work and Go package reuse; v0.2.4 semantic checking after edits was still project-wide. Failed checksum verification preserves the previous executable. [Final build samples](tests/performance/results/parallel-build-windows-amd64-go1.26.3.csv).

The subsequent namespace semantic cache was compared directly with the published v0.2.4 compiler using `ghi check` on the same DDD project, machine and Go version. Five measured pairs after one warm-up pair per compiler gave:

| Check scenario | v0.2.4 median | Namespace cache median |
| --- | ---: | ---: |
| Unchanged sources | 1.685 s | 1.710 s |
| Edited application service | 1.722 s | 1.771 s |

This dependency-heavy example shows **no end-to-end speedup** from namespace reuse in this run: dependency preparation and checksum verification dominate the overlapping pipeline. The changed-source ranges were 1.652–1.783 s before and 1.715–1.797 s after. A separate behavioral check confirms that editing only the entry namespace rechecks that namespace and reuses three unchanged namespaces, while an inheritance change rechecks the affected component. These results establish selective semantic work, not a promise of lower wall time on every project. [Check samples](tests/performance/results/namespace-check-windows-amd64-go1.26.3.csv).

Dependency preparation now edits the staged module with `golang.org/x/mod`, avoiding two Go subprocesses. Go still downloads/resolves the selected modules and checks download sums. Ghi verifies every ZIP entry and extracted source file against the locked `h1` checksum, with bounded parallel file reads and directory enumeration that avoids a separate stat for every file. Verification is performed on every compilation, including cache hits; it is not bypassed based on timestamps. Damaged archives or extracted contents prevent accepting output and preserve the previous executable.

A final comparison against the namespace-cache implementation, using the same five-sample DDD `ghi check` protocol, measured 1.714 → 1.773 s unchanged and 1.788 → 1.849 s after an edit. The final pipeline also includes cancellation of subprocess trees. Although dependency preparation removes redundant Go commands and uses parallel hashing, this complete Windows run was about 3% slower; it does not establish an end-to-end compilation speedup. These are warm-cache Windows amd64 results, not fresh-install or cross-platform guarantees. [Dependency preparation samples](tests/performance/results/dependency-verification-windows-amd64-go1.26.3.csv).

Version v0.2.6 reuses the 32 KiB copy buffers used for module archive/source hashing instead of allocating one for every file. A CPU profile identified allocation/GC work alongside decompression and filesystem operations. All bytes are still read and checked on every compilation, and subprocess-tree cancellation remains enabled. Against the published v0.2.5 compiler, seven measured samples after warm-up gave:

| DDD `ghi check` | v0.2.5 median | Buffer reuse median |
| --- | ---: | ---: |
| Unchanged | 2.075 s | 1.582 s |
| Edited service | 2.096 s | 1.665 s |

This run was about 24% and 21% faster respectively. Each compiler used a separate identical project checkout and its own Ghi build cache; invocation order alternated to reduce ordering bias. Module/Go caches were warm; other system activity was not controlled. Windows amd64 and Go 1.26.3 were used; results do not establish cross-platform gains. [Raw samples](tests/performance/results/check-buffer-reuse-windows-amd64-go1.26.3.csv).

### HTTP API and SQLite comparison

The [HTTP benchmark](benchmarks/http-api/run.py) runs the actual `examples/ddd-api` Ghi server against a [read-only Go counterpart](benchmarks/http-api/go/main.go) for three users endpoints. The counterpart implements only the measured routes and inputs, not the complete DDD application. Both use the same Chi, UUID and SQLite versions, SQL, user-model normalization, JSON response fields, request-ID/logging/recovery/timeout middleware and one SQLite connection. Info logging is disabled for both. Each server starts with an independent copy of the same 1,000-user SQLite database.

Measured on Windows amd64 with Go 1.26.3 and `GOMAXPROCS=8`: three samples of 20,000 requests per language, endpoint and concurrency level, with 300 warmup requests before each sample. Server order alternates between samples. The client uses localhost HTTP/1.1 keep-alive, validates the status and exact JSON bytes of every response, and uses Windows QueryPerformanceCounter for latency. All 720,000 measured responses passed validation. [Raw samples and source hashes](tests/performance/results/http-ddd-windows-amd64-go1.26.3.json).

| Endpoint | Concurrent clients | Ghi requests/s | Go requests/s | Ghi / Go p95 latency |
| --- | ---: | ---: | ---: | ---: |
| List 20 users | 1 | 5,004 | 4,901 | 0.312 / 0.328 ms |
| Get one user | 1 | 6,571 | 6,789 | 0.237 / 0.225 ms |
| Missing user, expected 404 | 1 | 5,960 | 6,824 | 0.274 / 0.224 ms |
| List 20 users | 16 | 8,886 | 8,870 | 5.026 / 4.970 ms |
| Get one user | 16 | 11,788 | 12,766 | 3.746 / 3.451 ms |
| Missing user, expected 404 | 16 | 13,985 | 14,019 | 3.091 / 3.142 ms |

These are medians across samples. In this setup, list throughput is similar and successful single-user reads are about 3% to 8% lower for Ghi. The single-client 404 case is about 13% lower; Ghi captures exception stacks while the Go counterpart returns ordinary errors. Median process peak working sets across scenarios are approximately 22.0–22.6 MiB for Ghi and 21.6–22.3 MiB for Go, including startup and warmup. This is not per-request allocation or retained-heap measurement.

The benchmark is a closed-loop read workload: client and server share a machine, SQLite serializes access through one connection, and filesystem/database caches are warm. It does not measure writes, remote databases, sustained production capacity, open-loop tail latency or the entire language's overhead. Run-to-run variation and the shared bottlenecks can hide small differences.

To reproduce, install the DDD example's locked dependencies with `mojave install` from `examples/ddd-api`, then run `python benchmarks/http-api/run.py` from the repository root with Go and Python available. The runner builds both servers and the client, uses disposable databases under `.work`, and checks the Go counterpart's module versions against `mojave.lock`. Results default to `.work/http-ddd-results.json`; use `--output` to choose another destination.

### DDD not-found handling

The development DDD example now uses `QueryContext`, `Next` and `Err` for single-row lookups in both repositories. An empty result directly throws the domain `NotFoundError`, avoiding the intermediate `GoError` and its stack that `QueryRowContext(...).Scan(...)` produced for `sql.ErrNoRows`. Query, iteration, scan and close failures still become `GoError`; cancellation and a closed database are not treated as missing records. Rows are closed before constructing a successful result, with deferred cleanup for exceptional paths. The final domain exception retains its code, message and a stack pointing to `Find`.

This is an example-level optimization compatible with Ghi v0.2.3; it does not change the compiler's native-error bridge or exception semantics. In a direct missing-row benchmark using empty SQLite databases, Go 1.26.3 and `GOMAXPROCS=8`, five alternating before/after samples gave these medians. [Repository samples](tests/performance/results/ddd-404-repository-windows-amd64-go1.26.3.csv).

| Missing-row lookup | Before | After | Before / after bytes | Before / after allocations |
| --- | ---: | ---: | ---: | ---: |
| User repository | 26.23 µs | 22.99 µs | 1920 / 1488 | 44 / 40 |
| Task repository | 26.25 µs | 24.90 µs | 2016 / 1584 | 46 / 42 |

The [repository fixture](benchmarks/http-api/testdata/repository.ghi) replaces `main.ghi` in disposable copies of the before/after DDD project. Both were compiled with the released Ghi v0.2.3 compiler. Run each executable with `-test.benchtime=300ms`, a separate empty database path and the migrations directory as its final two arguments. Construction and migrations occur outside the measured loops; each loop checks the expected domain error code.

An HTTP comparison of the same two implementations ran five alternating samples of 10,000 requests per scenario and concurrency level, with 300 warmup requests. All 400,000 measured responses matched the expected status and exact JSON bytes. [HTTP before/after samples](tests/performance/results/http-ddd-404-before-after.json).

| HTTP workload | Clients | Before requests/s | After requests/s | Before / after p95 |
| --- | ---: | ---: | ---: | ---: |
| Get one user | 1 | 6,127 | 6,590 | 0.270 / 0.253 ms |
| Missing user, expected 404 | 1 | 6,218 | 6,708 | 0.255 / 0.235 ms |
| Get one user | 16 | 14,433 | 15,489 | 3.021 / 2.812 ms |
| Missing user, expected 404 | 16 | 13,538 | 13,575 | 3.283 / 3.251 ms |

Single-client 404 throughput is about 8% higher by median in this run; at 16 clients it is effectively unchanged. Samples overlap substantially, so these figures do not establish a universal or statistically significant HTTP speedup. The repeatable structural saving is one fewer intermediate exception and four fewer allocations per missing-row lookup. Successful reads also avoid the surrounding catch wrapper. The existing SQLite, localhost and closed-loop limitations still apply.

The HTTP runner also accepts `--baseline-ghi /path/to/old-server` to compare two Ghi binaries, and `--scenarios get missing` to focus on successful and missing single-user lookups. Current-source hashes describe the new server; binary hashes identify both executables. The old server must implement the same measured routes and accept the DDD environment variables.

## Files, namespaces and imports

Files use UTF-8 and the `.ghi` extension. Each file starts with `namespace`. Files in one directory share a namespace; imports are local to each file. The executable entry point is `func main()` in namespace `main`.

Ghi imports use dotted paths:

```text
import application.users
import application.users as users
import application.users.Service as UserService
import arm092.migrations.Migrator
import someone.migrations.Migrator as OtherMigrator
```

A namespace import exposes its members through the namespace name or alias. A selected type import exposes a class, interface or named type directly, allowing `new UserService(...)`. Aliases resolve name collisions. Imports must be used; duplicate local bindings and dependency cycles are rejected.

Native Go imports retain a quoted `go:` path:

```text
import fmt "go:fmt"
import http "go:net/http"
import chi "go:github.com/go-chi/chi/v5"
```

Quoted Ghi namespace imports are not supported. Package identity and installation are described under [Mojave packages](#mojave-packages).

## Types and collections

Ghi uses static types and Go-style inference: `name := "Ada"`, `var count int = 0`, `const limit = 10`. Primitive types include `bool`, `string`, integer and floating-point types. Arrays, slices, maps, channels, native structs, pointers and function types use Go forms. Comments use `//` or `/* ... */`.

```ghi
namespace main

func main() {
	names := []string{}
	names = append(names, "Ada")

	scores := map[string]int{}
	scores["Ada"] = 10

	fixed := [3]int{1, 2, 3}
	buffer := make([]byte, 16)
	lookup := make(map[string]int)
	lookup["first"] = fixed[0]
	println(names[0], scores["Ada"], len(buffer), lookup["first"])
}
```

`[]T{}` is an empty slice; `[N]T{...}` is a fixed-size array; `map[K]V{}` is an initialized empty map. `make` allocates slices, maps or channels. A nil map cannot be written to until initialized. Zero-filled allocations containing nonnullable Ghi objects or unconstrained generic values are restricted: initialize those values explicitly, for example by appending constructed objects.

## Enums

Enums are available starting with Ghi v0.2.2.

A plain enum declares its own type. Pass its cases to parameters of that type; strings and cases of another enum are not assignable. Its zero value is the first declared case. Plain cases support equality, map keys and generic `comparable` constraints; construct values using declared cases, not casts or composite literals. Plain cases are runtime values, so they cannot initialize a `const`.

```ghi
enum Direction {
	North,
	South,
}

func move(direction Direction) string {
	return match direction {
		Direction.North => "north",
		default => "south",
	}
}
```

For explicitly assigned values, declare the backing type: `string`, `int` or `bool`. Every case must have a literal value of that type. These cases are ordinary typed constants and can be passed directly to native Go or Ghi functions accepting the backing type.

```ghi
enum Status string {
	Pending = "pending",
	Done = "done",
}

enum HttpCode int {
	OK = 200,
	NotFound = 404,
}

enum SwitchState bool {
	On = true,
	Off = false,
}

func report(status string, code int, enabled bool) {
	println(status, code, enabled)
}

func main() {
	report(Status.Pending, HttpCode.OK, SwitchState.On)
}
```

Case definitions are immutable: `Status.Pending = "other"` and `Direction.North = Direction.South` are errors. A local variable initialized from a case remains assignable: `status := Status.Pending; status = "custom"` is valid. A backed enum name is an alias for its backing type, so a `Status` parameter also accepts arbitrary strings; it does not validate membership.

Enums are declared at namespace scope and must contain at least one case. Case names and values must be unique. Assigned values without a backing type, missing values in a backed enum, and mismatched literal types are rejected. Enum cases work with the existing `match` syntax, which continues to require a final `default` arm.

## Classes and inheritance

Classes have fields, constructors and methods. Visibility defaults to `private`; `protected` allows descendant access and `public` allows external access. Ghi supports single class inheritance and multiple implemented interfaces.

```ghi
namespace main

class Person {
	protected name string

	constructor(name string = "Guest") {
		this.name = name
	}

	public func describe() string {
		return this.name
	}
}

class Developer extends Person {
	constructor(name string) {
		parent(name)
	}

	public override func describe() string {
		return parent.describe() + " writes Ghi"
	}
}

func main() {
	developer := new Developer("Ada")
	var person Person = developer
	println(person.describe())
}
```

- `this` refers to the current object.
- `parent(...)` initializes the parent; `parent.method(...)` invokes its implementation on the same object.
- `override` is required when overriding a parent method. Its signature must be compatible and visibility cannot be reduced.
- Calls through a parent reference use the concrete object's override.
- Both `new Person("Ada")` and `Person("Ada")` construct an object.
- Trailing parameters may have defaults. Required parameters cannot follow optional parameters. Method overloading is not supported.
- An omitted parent constructor call is allowed when the parent can be initialized without arguments.

## Interfaces and generics

Interfaces describe method contracts. Classes are nominal types; interfaces are structural. An explicit `implements` clause checks and documents the intended contract. A matching public method set can satisfy an interface without the clause.

```ghi
namespace main

interface Identifiable {
	func getId() int
}

class User implements Identifiable {
	public func getId() int {
		return 7
	}
}

class Repository[T Identifiable] {
	public func identify(item T) int {
		return item.getId()
	}
}

class UserRepository extends Repository[User] {}

func main() {
	repository := new UserRepository()
	println(repository.identify(new User()))
}
```

`T` is a type parameter. `Repository[User]` substitutes `User` for `T`; `T Identifiable` requires the interface's public methods with compatible signatures. Private or protected methods do not satisfy an interface bound.

Type parameters are supported on functions, named types, classes and interfaces. Constraints include `any`, `comparable`, named interfaces and Go-style type sets. Parent arguments are explicit, including `Child[T any] extends Base[T]` and chained substitutions. Distinct generic class instantiations remain distinct types.

Multiple bounds can be combined with `type Entity interface { Identifiable; Labelled }`, then used as `[T Entity]`. Inline `[T interface { Identifiable; Labelled }]` also works. Embedded generic interfaces such as `Reader[int]` preserve their argument types in method calls and defaults. See the runnable [constraints](examples/constraints) and [inheritance](examples/inheritance) examples.

Individual methods cannot introduce additional type parameters. Generic fields and locals need initialization because `T` may represent a nonnullable object.

## Nullable values

Ghi object references are nonnullable by default. Put `?` **before** the type to permit absence: `?User`, `[]?User`, `map[string]?User`. Check for `nil` before accessing a nullable object.

```ghi
namespace main

class User {
	public name string
	constructor(name string) {
		this.name = name
	}
}

func findUser(found bool) ?User {
	if found {
		return new User("Ada")
	}

	return nil
}

func main() {
	user := findUser(true)
	if user != nil {
		println(user.name)
	}
}
```

Constructors must initialize nonnullable fields on every successful path. The compiler tracks proven non-null values; reassignment and closure writes can invalidate that proof. Map lookups and channel receives involving Ghi objects or type parameters preserve absence through nullable results.

## Functions and closures

Named functions use `func`. Anonymous functions can use Go-style `func(...) ... { ... }` or typed block arrows. Both capture variables from the surrounding lexical scope automatically; no capture list is required.

```ghi
namespace main

func main() {
	total := 0
	add := (value int) int => {
		total += value

		return total
	}

	read := func() int {
		return total
	}
	println(add(2), add(3), read())
}
```

Arrow bodies are blocks, for example `() => { ... }`. Parameters are explicitly typed; return types follow the parameter list. Captured mutable variables are shared with the enclosing scope, so concurrent access still needs synchronization. Function values and bound method values are supported.

## Exceptions and native Go errors

Use `throw`, typed `catch` and optional `finally`. Throwable classes inherit from `Exception`.

```ghi
namespace main

class ValidationError extends Exception {
	constructor(message string, code int = 0) {
		parent(message, code)
	}
}

func main() {
	try {
		throw new ValidationError("Name is required", 422)
	} catch err ValidationError {
		println(err.code, err.typeName, err.message)
		for _, frame := range err.stackTrace {
			println(frame.functionName, frame.file, frame.line)
		}
	} finally {
		println("Finished")
	}
}
```

| Exception field | Type | Meaning |
| --- | --- | --- |
| `code` | `int` | Application error code; default `0` |
| `typeName` | `string` | Fully qualified concrete exception type |
| `message` | `string` | Human-readable explanation |
| `stackTrace` | `[]StackFrame` | Original Ghi function, file and line information |

`Exception(message = "", code = 0)` is directly constructible. The trace is captured at the first throw; rethrowing the same object preserves it. Catches are considered in source order. `finally` runs on normal completion and exception unwinding.

Native Go calls with a trailing `error` automatically raise `GoError` for non-nil errors and yield the other results on success:

```ghi
namespace main

import strconv "go:strconv"

func main() {
	try {
		value := strconv.Atoi("42")
		println(value)
	} catch err GoError {
		println(err.message)
	}
}
```

`GoError` preserves the native error as `cause` and defaults to code `0`. Go runtime faults are not ordinary catchable Ghi exceptions. Handle exceptions inside the goroutine that can throw them; an outer goroutine's catch cannot intercept them.

## Control flow and match

Use Go-style `if`, `for`, `range`, `switch`, `select`, `break`, `continue`, `return` and `defer`. Conditions do not require parentheses. Newline and semicolon rules follow Go, so opening braces normally stay on the declaration or condition line.

`match` is a value expression:

```ghi
namespace main

func main() {
	status := 201
	label := match status {
		200, 201 => "success",
		404 => "not found",
		default => "other",
	}
	println(label)
}
```

The subject is evaluated once. Only the selected result expression is evaluated. Arms must produce compatible result types. A final `default` is required. Nested matches work; destructuring, guards, type patterns and block arms are not implemented.

## Concurrency

The keyword is `go`. Ghi uses Go runtime goroutines, channels and synchronization libraries:

```ghi
namespace main

func main() {
	values := make(chan int, 1)
	go func() {
		values <- 42
	}()
	println(<-values)
}
```

Use `select` to coordinate channel operations. Native goroutine and deferred calls capture their arguments when scheduled. Ghi does not make unsynchronized shared writes safe.

## Go interoperability

Import Go's standard library directly with `go:` paths. Install third-party modules through Mojave, then import their Go module paths. Native functions, structs, interfaces and callbacks are available within the compiler's supported interoperability rules. The trailing-error bridge described above applies to native calls.

For example, the [DDD API](examples/ddd-api) uses the chi router, SQLite, HTTP controllers, application services and repositories. Ghi's custom object representation is not a drop-in replacement for arbitrary native Go structs; conversions and callback signatures must match the receiving API.

## Mojave packages

Mojave is developed in its own public [repository](https://github.com/arm092/mojave) and has an independent version. A compatible version is included with the compiler. It installs Ghi libraries from Git repositories and Go libraries through the Go toolchain. Run commands in your project directory. `mojave add` creates `mojave.json` automatically if it does not exist; you do not need to create it by hand. Failed dependency resolution does not leave a newly created manifest behind.

```sh
mojave add arm092/migrations https://github.com/arm092/ghi-migrations.git v0.2.0
mojave add go:github.com/go-chi/chi/v5@v5.3.2
mojave install
mojave update
```

The `arm092/migrations` package identity maps to namespace `arm092.migrations`. Its selected class import is `import arm092.migrations.Migrator`. Different owners can publish packages with the same short name.

| Path | Purpose | Commit it? |
| --- | --- | --- |
| `mojave.json` | Requested Ghi and Go dependencies | Yes |
| `mojave.lock` | Resolved versions/commits and integrity information | Yes |
| `.ghi/packages/<namespace>` | Installed Ghi package source | No |
| `.ghi/` | Local dependency and generated operation state | No |

Go dependencies use the Go module cache. Mojave's manifest and lock describe dependencies; generated Go modules are build details, so a Mojave project does not need hand-maintained `go.mod` or `go.sum` files.

`install` replays an existing lock. `update` explicitly resolves requested refs again. Git refs may be exact tags, branches or commits; version ranges such as `^1.2` are not supported. Existing verified Ghi package caches can be reused offline. Git dependencies require Git on PATH and repository access. There is no central Ghi package registry or package install script hook.

Remove dependencies with `mojave remove arm092/migrations` or `mojave remove go:github.com/go-chi/chi/v5`. Edit libraries in their own repositories, not inside `.ghi/packages`.

## Testing and formatting

Application tests belong in a separate project-root `tests/` directory. Production builds exclude that directory, and production namespaces cannot import test namespaces. Tests use Go's `testing` package through native imports; see the [DDD tests](examples/ddd-api/tests) for unit and integration examples.

```sh
ghi test .
ghi test -v -run User .
ghi fmt .
ghi fmt --check .
ghi check .
```

Ghi v0.2.2 also supports `ghi check --stdin --filename /absolute/project/main.ghi /absolute/project` for editor integrations. Send UTF-8 source on standard input. The buffer replaces that existing production file only in memory; other sources and dependencies come from the project, and diagnostics retain the original path and source positions. This does not create new files or check files excluded from production, including `tests/`. Flags precede the optional project directory.

The canonical formatter sorts imports by path, uses tabs, expands nonempty blocks, indents switch/select cases, and puts collection entries on separate lines. It inserts a blank line before `return` when another statement precedes it in the same block. Empty blocks remain `{}`. GoLand's Reformat Code uses this formatter on the editor buffer.

## GoLand support

The plugin is developed in the separate [ghi-goland repository](https://github.com/arm092/ghi-goland). Its [JetBrains Marketplace submission](https://plugins.jetbrains.com/plugin/34508-ghi) is awaiting moderation. After approval, search for **Ghi** under **Settings → Plugins → Marketplace**.

The [v0.1.1 plugin ZIP](https://github.com/arm092/ghi-goland/releases/tag/v0.1.1) is already available on GitHub; download it and use **Settings → Plugins → Install Plugin from Disk**. Configure the compiler path and project directory under **Languages & Frameworks → Ghi**. Check the plugin descriptor's IDE build compatibility before installing into a different GoLand version.

The plugin provides:

- `.ghi` file recognition, the yellow Armenian **Ղ** icon and syntax colors.
- Completion, navigation, parameter hints and rename assistance.
- Class autoimport, import shortening and collision-safe aliases.
- Generic inheritance and embedded interface constraint assistance.
- Enum declaration/case highlighting, completion and navigation.
- Improved expression inference for chained calls and generic fields/methods.
- Canonical Reformat Code and compiler diagnostics, including unsaved editor buffers with a compatible development compiler.
- Build, Run and Debug actions.
- Original-source breakpoints, stepping, stacks and variables through GoLand's bundled Delve.

Enum compilation and unsaved-buffer checking require Ghi v0.2.2 or later.

Native Go assistance uses the configured SDK and locally installed dependencies. Type inference is still partial; compiler checking remains authoritative. Debug builds can also be created with `ghi build --debug .`.

## Examples and current boundaries

| Project | Demonstrates |
| --- | --- |
| [Enums](examples/enums) | Plain enum types, string/int/bool constants, imports and match. |
| [Hello](examples/hello) | Minimal executable |
| [Objects](examples/objects) | Classes, interfaces and inheritance |
| [Inheritance](examples/inheritance) | Generic repositories and specialization |
| [Constraints](examples/constraints) | Imported and composite interface bounds |
| [Task API](examples/task-api) | Small HTTP service, SQLite persistence, published migration package and integration tests |
| [DDD API](examples/ddd-api) | Routes, controllers, services, repositories, models, migrations and tests |

From a cloned checkout, use `ghi run examples/hello`. For projects with dependencies, run `mojave install` inside that example first.

### Published-package service

The [Task API](examples/task-api) is verified with the published **Ghi v0.2.4**, **Mojave v0.1.0** and **arm092/migrations v0.4.0** on Windows amd64. Its lockfile pins the migration package commit and Go dependency checksums. No local package checkout or unpublished compiler is required.

```sh
cd examples/task-api
mojave install
ghi test .
ghi run .
```

The server listens on `127.0.0.1:8080`. `POST /tasks` accepts `{"Title":"Ship Ghi"}` and returns the created task with status 201; `GET /tasks` returns the task list. Empty or overlong titles return 422, malformed JSON returns 400, and storage failures return a generic 500 response. The handler, service and repository live in `httpapi/`, `service/` and `store/`; tests are under `tests/integration/`.

Configuration uses `GHI_TASK_ADDR`, `GHI_TASK_DB` (default `tasks.db`) and `GHI_TASK_MIGRATIONS`. SQL migrations live in `store/migrations/`, including an explicit rollback file. Startup applies pending migrations through the published `Migrator`; replay preserves existing data and checksum mismatches or migration failures stop startup. Tests use temporary databases and cover HTTP behavior, persistence after reopen, migration replay, rollback/reapply, atomic failure and a sanitized storage error response.

For deployment, build with `ghi build -o bin/task-api .`, copy the binary and `store/migrations/`, and set `GHI_TASK_MIGRATIONS` to the deployed SQL directory. The source-tree migration path is only a development default. Use an `.exe` output name on Windows. Rollbacks are explicit maintenance operations through `Migrator.Down`; the example never runs them automatically.

### Standalone request journal

[Request Journal](services/request-journal) is a standalone backend outside the compiler examples. It tracks operational requests, their status and an ordered history of status changes. The source is split into `domain/`, `application/`, `storage/` and `httpapi/`; SQL migrations are in `migrations/` and tests in `tests/`.

The fresh-consumer workflow has been verified on Windows amd64 with the Ghi v0.2.6 release binary, published **Mojave v0.1.0**, **arm092/migrations v0.4.0** and [**arm092/validation v0.1.0**](https://github.com/arm092/ghi-validation/releases/tag/v0.1.0), using only the committed manifest and lockfile. It uses chi v5.3.2 and modernc SQLite v1.59.0. Copy this directory to use it independently:

```sh
cd services/request-journal
mojave install
ghi test .
ghi run .
```

| Method and route | Behavior |
| --- | --- |
| `GET /health` | Database readiness |
| `POST /requests` | Create with `{"title":"Restore notifications"}`; returns 201 |
| `GET /requests?status=open&limit=20&offset=0` | Filtered, paginated list, newest first |
| `GET /requests/{id}` | Read one request |
| `PUT /requests/{id}` | Replace title and status, e.g. `{"title":"Restored","status":"resolved"}` |
| `GET /requests/{id}/history` | Ordered status history |
| `DELETE /requests/{id}` | Delete request and its history; returns 204 |

Statuses are `open`, `in_progress` and `resolved`; reopening is supported. New requests start as `open`. Updating a title without changing status does not add a history event. Request changes and history insertion share a transaction. Queries use bound SQL parameters. Foreign keys and the busy timeout are configured for every SQLite connection, including replacements after cancellation.

The API returns JSON: malformed/oversized bodies and unknown fields return 400, invalid values return 422, missing resources return 404 and internal failures return a generic 500. Titles contain 1–200 characters after trimming and cannot include NUL; pagination accepts a limit of 1–100 and a nonnegative offset. Bodies are limited to 4 KiB. The service binds to localhost by default and has no authentication layer.

Validation uses the published `arm092/validation` package. Title length counts Unicode code points. Semantic checks collect all violations before accessing storage, and a rejected update preserves both the record and its history. Each 422 response includes field names and stable codes, for example:

```json
{"error":"invalid request fields","fields":[{"field":"title","code":"required"},{"field":"title","code":"min_length"},{"field":"status","code":"one_of"}]}
```

Independent rules may report more than one violation for a field. Create requests reject an explicit nonempty status with `initial_status`; NUL characters produce `nul_character`. Invalid integer syntax stops at the conversion boundary with `integer` for pagination or `positive_integer` for an ID; semantic checks run after conversion succeeds.

| Environment variable | Default |
| --- | --- |
| `JOURNAL_ADDR` | `127.0.0.1:8080` |
| `JOURNAL_DB` | `journal.db` (file path) |
| `JOURNAL_MIGRATIONS` | `migrations` (relative to the working directory) |

Startup applies pending migrations, and a migration failure prevents the listener from opening. Run from the project directory, or set absolute database/migration paths. For deployment, use `ghi build -o bin/journal .` (`bin/journal.exe` on Windows), then copy the executable and `migrations/`. Go and Ghi are not needed to run the binary. Database files should live on persistent storage. Shutdown handles interrupt/SIGTERM with a five-second HTTP grace period.

`ghi test .` checks atomic rollback on history failure, connection replacement after cancellation, cascading deletion and sanitized storage errors. The Python 3 smoke runner copies the project into a fresh temporary directory, installs locked dependencies, runs Ghi tests, builds and starts the executable, exercises real HTTP requests including concurrent writes, restarts against the same database, and checks failed migration rollback:

```sh
python tests/smoke.py --ghi /absolute/path/to/ghi --mojave /absolute/path/to/mojave
```

### Compiler fixes in v0.2.5

Generic numeric fields support compound assignment and increment/decrement, including inherited access, without treating compiler-generated field addresses as nullable. Generated forwarding methods use private parameter names, so a parent method parameter named `U` or `Item` does not collide with a descendant type parameter or concrete type of the same name. These fixes preserve source-level names and apply in normal and debug builds.

Generic constructor constraint errors retain the source type argument location. Parent/interface lookup, inheritance cycles and override errors report the relevant Ghi declaration location; constructor and method call diagnostics display source names rather than generated wrapper names. These diagnostics and generic fixes are included in v0.2.5.

Current boundaries include single class inheritance, no method overloading, no per-method type parameters, and the match restrictions listed above. Browser execution is not a target. Published binary bundles currently cover Windows and macOS; native macOS verification is limited to the Apple silicon installation, command and generated-project checks described above. Ghi source semantics are the public interface; generated Go code is not a supported package API.

### Building the tools from source

With a compatible Go toolchain installed (see [go.mod](go.mod)):

```sh
go build -o bin/ghi ./cmd/ghi
go build -ldflags="-X main.version=v0.1.0" -o bin/mojave github.com/arm092/mojave/cmd/mojave
go test ./...
go vet ./...
```

Use `.exe` output names on Windows. Compiler tests currently include Go package tests and external language/CLI/debugger suites under [tests](tests). This differs from the application convention requiring Ghi tests in the application's `tests/` directory.
