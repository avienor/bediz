// Command release builds the reproducible release archives for one tagged
// Bediz version:
//
//	go run ./tools/release [-out DIR] vX.Y.Z
//	go run ./tools/release -check vX.Y.Z
//
// Run it from a clean checkout of the tag with the Go toolchain pinned in
// go.mod. The default release directory is dist/<version>. With -check, it
// verifies the release preconditions and builds nothing.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/avienor/bediz/internal/release"
)

func main() {
	out := flag.String("out", "", "release directory (default dist/<version> in the repository)")
	check := flag.Bool("check", false, "verify the release preconditions without building")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: go run ./tools/release [-out DIR] vX.Y.Z\n       go run ./tools/release -check vX.Y.Z")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 || (*check && *out != "") {
		flag.Usage()
		os.Exit(2)
	}
	version := flag.Arg(0)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	output, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "release: find repository root:", err)
		os.Exit(1)
	}
	root := strings.TrimSpace(string(output))
	if *check {
		if err := release.Check(ctx, root, version); err != nil {
			fmt.Fprintln(os.Stderr, "release:", err)
			os.Exit(1)
		}
		return
	}
	opts := release.Options{Root: root, Version: version, OutDir: *out, Log: os.Stderr}
	if opts.OutDir == "" {
		opts.OutDir = filepath.Join(opts.Root, "dist", version)
	}
	if err := release.Build(ctx, opts); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}
