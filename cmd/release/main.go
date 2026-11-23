// Command release builds and publishes release artifacts.
//
//	release manifest  generate release.json from the release assets
//	release upload    upload the assets and the latest manifest to S3-compatible storage
//
// The two subcommands are kept separate on purpose: generating the manifest
// needs no credentials, uploading does, and CI regenerates the manifest after
// the release goes public to fill in published_at.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "manifest":
		err = runManifest(os.Args[2:])
	case "upload":
		err = runUpload(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: release <command> [flags]")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  manifest  generate release.json from the release assets")
	fmt.Fprintln(os.Stderr, "  upload    upload the assets and the latest manifest to S3-compatible storage")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, `run "release <command> -h" for the flags of a command`)
}
