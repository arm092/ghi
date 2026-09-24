# Ghi – Go, Hierarchy, Interfaces

Ghi is a statically typed language for backend applications. It combines Go-like syntax and the Go runtime with classes, inheritance, constructors, typed exceptions, nullable types and namespaces.

Ghi compiles your project to Go, invokes the Go toolchain, and produces a native executable. Applications use Go's garbage collector, goroutines, channels and library ecosystem. There is no interpreter to install on the deployment machine.

**Current release:** [Ghi v0.2.1](https://github.com/arm092/ghi/releases/tag/v0.2.1), bundled with independently versioned [Mojave v0.1.0](https://github.com/arm092/mojave/releases/tag/v0.1.0). **IDE:** [Ghi for GoLand v0.1.0](https://github.com/arm092/ghi-goland/releases/tag/v0.1.0).

Ghi is an experimental, pre-1.0 language. Syntax and package contracts may change. This README documents the implemented language; the [examples](examples) provide runnable projects.

The language tools and GoLand plugin are released under the [MIT License](LICENSE).

## Contents

- [Installation](#installation)
- [Quick start](#quick-start)
- [How compilation works](#how-compilation-works)
- [Files, namespaces and imports](#files-namespaces-and-imports)
- [Types and collections](#types-and-collections)
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

Download the archive for your operating system and CPU from the [compiler release](https://github.com/arm092/ghi/releases/tag/v0.2.1).

| Platform | CPU | Archive suffix |
| --- | --- | --- |
| Windows | Intel/AMD 64-bit | `windows_amd64.zip` |
| Windows | ARM64 | `windows_arm64.zip` |
| macOS | Intel | `darwin_amd64.tar.gz` |
| macOS | Apple silicon | `darwin_arm64.tar.gz` |

Extract the complete archive, keeping both executables, their checksum files and the installer together. Outer archive hashes are in `checksums.txt` on the release page.

### Windows

Download and run [Ghi Setup](https://github.com/arm092/ghi/releases/download/v0.2.1/ghi_v0.2.1_windows_setup.exe). The wizard selects the native x64 or ARM64 binaries, installs Ghi and Mojave, prepares Go and adds the commands to your user PATH. No administrator access is required. Open a new terminal after installation.

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

Download the [v0.2.1 universal macOS installer (.pkg)](https://github.com/arm092/ghi/releases/download/v0.2.1/ghi_v0.2.1_macos_universal.pkg). It contains Intel and Apple silicon binaries for Ghi v0.2.1 and Mojave v0.1.0 and prepares Go for the signed-in user before installation. Installation, version commands, managed Go 1.26.8 setup, project creation and compilation/execution were verified on an Apple silicon Mac. The package is unsigned and has not been notarized by Apple. Its `.sha256` file is available in the release. Homebrew and `.pkg` are alternative installation methods; the package refuses to overwrite another installation. The [installer build kit](https://github.com/arm092/ghi/releases/download/v0.2.1/ghi_v0.2.1_macos_installer_kit.zip) and `scripts/package-macos.sh` provide the build recipe.

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

## How compilation works

1. Ghi loads `.ghi` sources, namespaces and installed Mojave dependencies.
2. It parses and validates Ghi constructs, then lowers them into Go declarations and expressions.
3. Type checking validates assignments, generic constraints, method signatures and Ghi-specific rules such as visibility and nonnullable initialization.
4. The compiler creates a temporary Go module and invokes the selected Go toolchain.
5. The result is an executable for the target platform. Debug builds retain mappings to the original Ghi files.

Generic types remain typed Go generics. Classes use generated interfaces and storage, preserving dynamic dispatch. Exceptions use generated runtime support. These transformations can introduce overhead; using the Go backend does not guarantee that every Ghi program performs identically to hand-written Go. Generated source and the generated object ABI are implementation details.

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

The development compiler also supports `ghi check --stdin --filename /absolute/project/main.ghi /absolute/project` for editor integrations. Send UTF-8 source on standard input. The buffer replaces that existing production file only in memory; other sources and dependencies come from the project, and diagnostics retain the original path and source positions. This does not create new files or check files excluded from production, including `tests/`. Flags precede the optional project directory. This API is not included in the published v0.2.1 binary.

The canonical formatter sorts imports by path, uses tabs, expands nonempty blocks, indents switch/select cases, and puts collection entries on separate lines. It inserts a blank line before `return` when another statement precedes it in the same block. Empty blocks remain `{}`. GoLand's Reformat Code uses this formatter on the editor buffer.

## GoLand support

The plugin is developed in the separate [ghi-goland repository](https://github.com/arm092/ghi-goland). Its [JetBrains Marketplace submission](https://plugins.jetbrains.com/plugin/34508-ghi) is awaiting moderation. After approval, search for **Ghi** under **Settings → Plugins → Marketplace**.

Until then, download the [v0.1.0 plugin ZIP](https://github.com/arm092/ghi-goland/releases/tag/v0.1.0) and use **Settings → Plugins → Install Plugin from Disk**. Configure the compiler path and project directory under **Languages & Frameworks → Ghi**. Check the plugin descriptor's IDE build compatibility before installing into a different GoLand version.

The plugin provides:

- `.ghi` file recognition, the yellow Armenian **Ղ** icon and syntax colors.
- Completion, navigation, parameter hints and rename assistance.
- Class autoimport, import shortening and collision-safe aliases.
- Generic inheritance and embedded interface constraint assistance.
- Canonical Reformat Code and saved-file compiler diagnostics.
- Build, Run and Debug actions.
- Original-source breakpoints, stepping, stacks and variables through GoLand's bundled Delve.

Native Go assistance uses the configured SDK and locally installed dependencies. Type inference is still partial; compiler checking remains authoritative. Debug builds can also be created with `ghi build --debug .`.

## Examples and current boundaries

| Project | Demonstrates |
| --- | --- |
| [Hello](examples/hello) | Minimal executable |
| [Objects](examples/objects) | Classes, interfaces and inheritance |
| [Inheritance](examples/inheritance) | Generic repositories and specialization |
| [Constraints](examples/constraints) | Imported and composite interface bounds |
| [Task API](examples/task-api) | Backend application with dependencies |
| [DDD API](examples/ddd-api) | Routes, controllers, services, repositories, models, migrations and tests |

From a cloned checkout, use `ghi run examples/hello`. For projects with dependencies, run `mojave install` inside that example first.

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
