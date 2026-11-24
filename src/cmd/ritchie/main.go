// Command ritchie provides the public developer tool.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"ritchie.dev/internal/driver"
)

func main() {
	status := dispatch(os.Args[1:], os.Stdout, os.Stderr,
		func(args []string) int {
			return driver.Main(args, driver.OSHost("ritchie compile", os.Stdout, os.Stderr))
		},
		func(args []string) int {
			h := driver.OSHost("ritchie run", os.Stdout, os.Stderr)
			h.Stdin = os.Stdin
			return driver.RunMain(args, h)
		},
		func(args []string) int {
			return driver.KernelMain(args, driver.OSHost("ritchie", os.Stdout, os.Stderr))
		})
	os.Exit(status)
}

func dispatch(args []string, stdout, stderr io.Writer, compile, run, kernel func([]string) int) int {
	var leading []string

options:
	for len(args) > 0 {
		switch {
		case args[0] == driver.NoTracebackOption || args[0] == driver.DebugOption:
			leading = append(leading, args[0])
			args = args[1:]
		case strings.HasPrefix(args[0], driver.NoTracebackOption+"="), strings.HasPrefix(args[0], driver.DebugOption+"="):
			fmt.Fprintf(stderr, "ritchie: unknown option %q\n", args[0])
			return driver.ExitFailure
		default:
			break options
		}
	}

	if len(args) == 0 {
		fmt.Fprint(stderr, driver.Usage())
		return driver.ExitFailure
	}
	forwarded := make([]string, 0, len(leading)+len(args)-1)
	forwarded = append(forwarded, leading...)
	forwarded = append(forwarded, args[1:]...)
	switch args[0] {
	case "--help", "-h":
		fmt.Fprint(stdout, driver.Usage())
		return driver.ExitOK
	case "compile":
		return compile(forwarded)
	case "run":
		return run(forwarded)
	case "kernel":
		return kernel(forwarded)
	default:
		fmt.Fprintf(stderr, "ritchie: unknown command %q\n", args[0])
		return driver.ExitFailure
	}
}
