// Command release-manifest generates dist/release.json for the in-app updater.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"

	"uiautodev-wails/internal/update"
)

func main() {
	dir := flag.String("dir", "dist", "directory containing release assets")
	version := flag.String("version", "", "release version without the v prefix")
	tag := flag.String("tag", "", "release tag (defaults to the version)")
	notesFile := flag.String("notes-file", "", "file containing release notes")
	releaseJSON := flag.String("release-json", "", "gh release view JSON with body, publishedAt and url")
	releaseURL := flag.String("release-url", "", "release page URL")
	publishedAt := flag.String("published-at", "", "release publication time")
	out := flag.String("out", "dist/release.json", "output manifest path")
	flag.Parse()

	notes := ""
	if *notesFile != "" {
		data, err := os.ReadFile(*notesFile)
		if err != nil {
			log.Fatal(err)
		}
		notes = string(data)
	}
	if *releaseJSON != "" {
		data, err := os.ReadFile(*releaseJSON)
		if err != nil {
			log.Fatal(err)
		}
		var release struct {
			Body        string `json:"body"`
			PublishedAt string `json:"publishedAt"`
			URL         string `json:"url"`
		}
		if err := json.Unmarshal(data, &release); err != nil {
			log.Fatal(err)
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
		log.Fatal(err)
	}
	if err := update.WriteManifest(*out, manifest); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (version %s, %d assets)", *out, manifest.Version, len(manifest.Assets))
}
