package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/uiautodev/uiautodev/internal/s3store"
	"github.com/uiautodev/uiautodev/internal/update"
)

// runUpload uploads the assets first and the latest manifest last, so clients
// never see a version whose assets are still uploading.
func runUpload(args []string) error {
	fs := flag.NewFlagSet("upload", flag.ExitOnError)
	var (
		dir       = fs.String("dir", "dist", "directory containing the release artifacts")
		version   = fs.String("version", "", "version number (v prefix allowed; defaults to the manifest version)")
		bucket    = fs.String("bucket", "uiautodev", "target bucket")
		prefix    = fs.String("prefix", "", "key prefix inside the bucket")
		manifest  = fs.String("manifest", "release.json", "file to also publish as the latest manifest")
		latestKey = fs.String("latest-key", "updates/latest.json", "bucket key of the latest manifest")
		guardURL  = fs.String("latest-guard-url", "", "URL of the online latest manifest, used to avoid rolling back the live version")
		endpoint  = fs.String("endpoint", os.Getenv("S3_ENDPOINT"), "S3 endpoint, e.g. https://<account>.r2.cloudflarestorage.com")
		region    = fs.String("region", "auto", "S3 region")
		dryRun    = fs.Bool("dry-run", false, "only print the upload plan, do not upload")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	manifestPath := filepath.Join(*dir, *manifest)
	release, err := update.ReadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest %s: %w", manifestPath, err)
	}

	resolvedVersion := strings.TrimPrefix(strings.TrimSpace(*version), "v")
	if resolvedVersion == "" {
		resolvedVersion = strings.TrimPrefix(strings.TrimSpace(release.Version), "v")
	}
	if resolvedVersion == "" {
		return fmt.Errorf("cannot determine version: -version not set and the manifest has no version")
	}

	items, err := planUploads(release.Assets, *dir, *prefix, resolvedVersion, *manifest)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("no artifacts to upload in %s", *dir)
	}

	var total int64
	for _, item := range items {
		total += item.Size
	}
	log.Printf("uploading %d files (%.1f MiB) to s3://%s/%s%s/",
		len(items), float64(total)/(1<<20), *bucket, *prefix, resolvedVersion)
	for _, item := range items {
		log.Printf("  %s -> %s", filepath.Base(item.Path), item.Key)
	}
	log.Printf("  then publish %s -> s3://%s/%s", *manifest, *bucket, *latestKey)

	if *dryRun {
		return nil
	}

	settings, err := s3store.Load(*endpoint, *region)
	if err != nil {
		return err
	}
	client, err := settings.Client()
	if err != nil {
		return err
	}
	uploader := transfermanager.New(client)
	ctx := context.Background()

	for _, item := range items {
		if err := uploadFile(ctx, uploader, *bucket, item); err != nil {
			return fmt.Errorf("upload %s: %w", item.Key, err)
		}
		log.Printf("uploaded %s", item.Key)
	}

	ok, reason := shouldPublishLatest(ctx, http.DefaultClient, *guardURL, resolvedVersion)
	log.Print(reason)
	if !ok {
		return nil
	}

	file, err := os.Open(manifestPath)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(*bucket),
		Key:          aws.String(*latestKey),
		Body:         file,
		ContentType:  aws.String(contentTypeFor(*manifest)),
		CacheControl: aws.String("no-cache"),
	}); err != nil {
		return fmt.Errorf("publish %s: %w", *latestKey, err)
	}
	log.Printf("published s3://%s/%s", *bucket, *latestKey)
	return nil
}

func uploadFile(ctx context.Context, uploader *transfermanager.Client, bucket string, item uploadItem) error {
	file, err := os.Open(item.Path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(item.Key),
		Body:        file,
		ContentType: aws.String(contentTypeFor(item.Path)),
	})
	return err
}
