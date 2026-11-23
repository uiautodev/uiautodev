// Command publish-release uploads release artifacts to S3-compatible storage
// (Cloudflare R2) and publishes updates/latest.json, the manifest used by
// in-app updates.
//
// The upload order is fixed: assets first, manifest last. This guarantees
// clients never see a version whose assets are still being uploaded. Only the
// files listed in the release.json manifest are uploaded.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/uiautodev/uiautodev/internal/s3store"
)

func main() {
	var (
		dir       = flag.String("dir", "dist", "directory containing the release artifacts")
		version   = flag.String("version", "", "version number (v prefix allowed; defaults to the manifest version)")
		bucket    = flag.String("bucket", "uiautodev", "target bucket")
		prefix    = flag.String("prefix", "", "key prefix inside the bucket")
		manifest  = flag.String("manifest", "release.json", "file to also publish as the latest manifest")
		latestKey = flag.String("latest-key", "updates/latest.json", "bucket key of the latest manifest")
		guardURL  = flag.String("latest-guard-url", "", "URL of the online latest manifest, used to avoid rolling back the live version")
		endpoint  = flag.String("endpoint", os.Getenv("S3_ENDPOINT"), "S3 endpoint, e.g. https://<account>.r2.cloudflarestorage.com")
		region    = flag.String("region", "auto", "S3 region")
		dryRun    = flag.Bool("dry-run", false, "only print the upload plan, do not upload")
	)
	flag.Parse()

	manifestPath := filepath.Join(*dir, *manifest)
	release, err := readManifest(manifestPath)
	if err != nil {
		log.Fatalf("failed to read manifest %s: %v", manifestPath, err)
	}

	resolvedVersion := strings.TrimPrefix(strings.TrimSpace(*version), "v")
	if resolvedVersion == "" {
		resolvedVersion = strings.TrimPrefix(strings.TrimSpace(release.Version), "v")
	}
	if resolvedVersion == "" {
		log.Fatal("cannot determine version: -version not set and the manifest has no version")
	}

	items, err := planUploads(release, *dir, *prefix, resolvedVersion, *manifest)
	if err != nil {
		log.Fatal(err)
	}
	if len(items) == 0 {
		log.Fatalf("no artifacts to upload in %s", *dir)
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
		return
	}
	settings, err := s3store.Load(*endpoint, *region)
	if err != nil {
		log.Fatal(err)
	}
	client, err := settings.Client()
	if err != nil {
		log.Fatal(err)
	}
	uploader := transfermanager.New(client)
	ctx := context.Background()

	for _, item := range items {
		if err := uploadFile(ctx, uploader, *bucket, item); err != nil {
			log.Fatalf("failed to upload %s: %v", item.Key, err)
		}
		log.Printf("uploaded %s", item.Key)
	}

	if ok, reason := shouldPublishLatest(ctx, http.DefaultClient, *guardURL, resolvedVersion); !ok {
		log.Print(reason)
		return
	} else {
		log.Print(reason)
	}

	file, err := os.Open(manifestPath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	if _, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(*bucket),
		Key:          aws.String(*latestKey),
		Body:         file,
		ContentType:  aws.String(contentTypeFor(*manifest)),
		CacheControl: aws.String("no-cache"),
	}); err != nil {
		log.Fatalf("failed to publish %s: %v", *latestKey, err)
	}
	log.Printf("published s3://%s/%s", *bucket, *latestKey)
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
