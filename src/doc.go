// Package ritchie provides the Ritchie compiler, local runner, and embedded
// runtime source bundle.
//
// # Commands
//
// The public command offers compilation and rapid local execution:
//
//	ritchie compile [--diagnostics=human|jsonl] [input]
//	ritchie run [--diagnostics=human|jsonl] [--serve[=PORT]] [input]
//	ritchie kernel print [--diagnostics=human|jsonl] [input]
//
// "ritchie kernel print" writes the kernel program that compilation builds
// from the input, which shows where remote reads overlap.
//
// The rcc command is equivalent to "ritchie compile", and the rcrun command is
// equivalent to "ritchie run". The run commands also accept standard input as a
// source file named "-":
//
//	printf 'main() { print("hello") }\n' | rcrun -
//
// Given --help or -h, each command writes its usage text, which lists these
// options and the input forms, to standard output and returns status 0.
// Command options may appear before or after the input. Human-readable
// diagnostics are the default. The JSON Lines form is intended for tools that
// consume structured diagnostics.
//
// A program writes print output to standard output and log output and error
// reports to standard error. The environment variable RITCHIE_LOG_LEVEL selects
// what reaches standard error: 0 writes nothing, 1 (the default) writes log
// output, the report of an error escaping main, and defect reports, 2 adds the
// report of each service invocation's escaping error, and 3 adds the report of
// an unrecovered failure inside a handler arm and the history lines of every
// report.
//
// # Selecting input
//
// With no input argument, the command searches from the working directory
// toward the filesystem root. The first directory containing exactly one
// *.ritchie.toml file selects that project. An explicit project-file argument
// selects that file without discovery.
//
// One or more .ri arguments select an ad-hoc source set. The listed files form
// one anonymous module in command-line order and may contain at most one main
// entry between them. Ad-hoc files and project files cannot be combined in one
// invocation.
//
// A project file maps module names to source-root directories:
//
//	[modules]
//	bank = "src/banking"
//	ledger = "src/ledger"
//
// Each regular .ri file beneath a root belongs to the module with the deepest
// matching root. Nested projects form separate project boundaries. Roots are
// relative to the project file, remain within its directory, and must assign at
// least one source file to their module.
//
// # Compilation results
//
// Compilation checks the complete selected source set before installing an
// artifact. The result depends on the program contents:
//
//   - A source set with main and no resident units produces a native executable.
//   - A source set with resident units produces a monolith .run bundle.
//   - A source set with neither main nor resident units is checked without
//     producing a runnable artifact.
//
// Project artifacts use the project filename without the .ritchie.toml suffix.
// Ad-hoc artifacts use the first source filename without its .ri suffix. The
// result is installed in the invocation's working directory. A rejected build
// leaves any prior complete artifact in place.
//
// A .run bundle contains the program and the platform information required to
// start it. It can be passed as the sole input to "ritchie run" or rcrun on a
// matching operating system and architecture.
//
// # Rapid local execution
//
// The run commands compile source with the same language and project rules as
// the compile commands, then start it on the local monolith target. Program
// input, output, and diagnostic streams are connected to those of the command.
// When a run command reads source from standard input, that input is consumed before
// the program starts, so the program reads end-of-file from its own input.
//
// Resident units are created before main starts. A resident-only source set
// requires --serve. With --serve, the application exposes its internal service
// routes over HTTP on port 7824 of the loopback interface, or on PORT with
// --serve=PORT, and remains active until signalled. Without --serve, only the external registrations declared by
// the source are exposed.
//
// # Diagnostics and exit statuses
//
// Diagnostics identify the source file and byte span to which they apply. A
// diagnostic also identifies the module after the file has been assigned to
// one. Diagnostics and command failures are written to standard error; program
// output belongs to the program.
//
// Compilation returns status 0 for accepted input, 1 for rejected source, and 2
// for an invocation or compiler failure. Rapid execution uses those statuses
// before startup and otherwise returns the program's status. If a signal ends
// the program, the run command returns 128 plus the signal number.
//
// # Embedded runtime sources
//
// RLRSources is an [embed.FS] containing the Ritchie language runtime
// and the monolith target packages. The bundle lets consumers
// materialize the runtime beside generated Go without locating
// separate runtime files on the host.
package ritchie
