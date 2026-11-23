package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/uiautodev/uiautodev/internal/update"
)

func runManifest(args []string) error {
	fs := flag.NewFlagSet("manifest", flag.ExitOnError)
	var (
		dir         = fs.String("dir", "dist", "directory containing release assets")
		version     = fs.String("version", "", "release version without the v prefix")
		tag         = fs.String("tag", "", "release tag (defaults to the version)")
		notesFile   = fs.String("notes-file", "", "file containing release notes")
		releaseJSON = fs.String("release-json", "", "gh release view JSON with body, publishedAt and url")
		releaseURL  = fs.String("release-url", "", "release page URL")
		publishedAt = fs.String("published-at", "", "release publication time")
		out         = fs.String("out", "dist/release.json", "output manifest path")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	notes := ""
	if *notesFile != "" {
		data, err := os.ReadFile(*notesFile)
		if err != nil {
			return fmt.Errorf("read notes file: %w", err)
		}
		notes = string(data)
	}
	if *releaseJSON != "" {
		data, err := os.ReadFile(*releaseJSON)
		if err != nil {
			return fmt.Errorf("read release JSON: %w", err)
		}
		var release struct {
			Body        string `json:"body"`
			PublishedAt string `json:"publishedAt"`
			URL         string `json:"url"`
		}
		if err := json.Unmarshal(data, &release); err != nil {
			return fmt.Errorf("parse release JSON: %w", err)
		}
		notes = release.Body
		if *releaseURL == "" {
			*releaseURL = release.URL
		}
		if *publishedAt == "" {
			*publishedAt = release.PublishedAt
		}
	}

	manifest, err := update.BuildManifest(*dir, *version, *tag, notes, *releaseURL, *publishedAt)
	if err != nil {
		return err
	}
	if err := update.WriteManifest(*out, manifest); err != nil {
		return err
	}
	log.Printf("wrote %s (version %s, %d assets)", *out, manifest.Version, len(manifest.Assets))
	return nil
}
