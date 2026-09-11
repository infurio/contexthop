// fixturecmd runs a command in the same disposable scenario used by Go tests.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/infurio/contexthop/internal/testenv"
)

func main() { os.Exit(run()) }

func run() int {
	binary := flag.String("binary", "", "absolute path to the checkout binary")
	scenario := flag.String("scenario", "acme", "fictional scenario")
	delay := flag.String("delay", "0", "provider delay in seconds")
	flag.Parse()
	if *binary == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "fixturecmd requires --binary PATH -- COMMAND [ARGS]")
		return 2
	}
	root, err := os.MkdirTemp("", "contexthop-fixture.")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(root)
	e, err := testenv.Create(root, testenv.Options{Scenario: *scenario, Binary: *binary, Delay: *delay})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// The reuse tape owns one background source session inside this environment.
	defer func() {
		b, err := os.ReadFile(filepath.Join(e.Home, "source-pid"))
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 1 {
				if process, err := os.FindProcess(pid); err == nil {
					_ = process.Signal(syscall.SIGTERM)
				}
			}
		}
	}()
	cmd := exec.Command(flag.Arg(0), flag.Args()[1:]...)
	cmd.Env = e.Environ()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err = cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case sig := <-signals:
				_ = cmd.Process.Signal(sig)
			case <-done:
				return
			}
		}
	}()
	if err = cmd.Wait(); err != nil {
		if exited, ok := err.(*exec.ExitError); ok {
			if status, ok := exited.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				return 128 + int(status.Signal())
			}
			return exited.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
