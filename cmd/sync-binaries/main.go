// Command sync-binaries syncs the binaries required for builds from
// S3-compatible storage into binaries/. It is kept separate from the upload
// path (cmd/publish-release) and only reads.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/uiautodev/uiautodev/internal/s3store"
)

func main() {
	var (
		dir         = flag.String("dir", "binaries", "local target directory")
		bucket      = flag.String("bucket", "uiautodev", "source bucket")
		prefix      = flag.String("prefix", "binaries/", "key prefix inside the bucket")
		endpoint    = flag.String("endpoint", os.Getenv("S3_ENDPOINT"), "S3 endpoint")
		region      = flag.String("region", "auto", "S3 region")
		concurrency = flag.Int("concurrency", 4, "number of concurrent downloads")
		dryRun      = flag.Bool("dry-run", false, "only list the files to download")
	)
	flag.Parse()

	settings, err := s3store.Load(*endpoint, *region)
	if err != nil {
		log.Fatal(err)
	}
	client, err := settings.Client()
	if err != nil {
		log.Fatal(err)
	}

	objects, err := listObjects(context.Background(), client, *bucket, *prefix)
	if err != nil {
		log.Fatalf("failed to list s3://%s/%s: %v", *bucket, *prefix, err)
	}

	items, skipped := planDownloads(objects, *dir, *prefix, localSize)
	log.Printf("remote objects: %d, already up to date locally: %d, to download: %d", len(objects), skipped, len(items))
	for _, item := range items {
		log.Printf("  %s -> %s (%d bytes)", item.Key, item.Path, item.Size)
	}
	if *dryRun || len(items) == 0 {
		return
	}

	if err := downloadAll(context.Background(), client, *bucket, items, *concurrency); err != nil {
		log.Fatal(err)
	}

	var total int64
	for _, item := range items {
		total += item.Size
	}
	log.Printf("sync complete: %d files, %.1f MiB", len(items), float64(total)/(1<<20))
}

func listObjects(ctx context.Context, client *s3.Client, bucket, prefix string) ([]remoteObject, error) {
	var objects []remoteObject
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, object := range page.Contents {
			if object.Key == nil {
				continue
			}
			objects = append(objects, remoteObject{
				Key:  *object.Key,
				Size: aws.ToInt64(object.Size),
			})
		}
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].Key < objects[j].Key })
	return objects, nil
}

func downloadAll(ctx context.Context, client *s3.Client, bucket string, items []downloadItem, concurrency int) error {
	if concurrency < 1 {
		concurrency = 1
	}

	jobs := make(chan downloadItem)
	errs := make(chan error, concurrency)
	var wg sync.WaitGroup

	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				if err := downloadFile(ctx, client, bucket, item); err != nil {
					errs <- fmt.Errorf("%s: %w", item.Key, err)
					return
				}
				log.Printf("downloaded %s", item.Key)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, item := range items {
			jobs <- item
		}
	}()

	wg.Wait()
	close(errs)
	return <-errs
}

func downloadFile(ctx context.Context, client *s3.Client, bucket string, item downloadItem) error {
	output, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(item.Key),
	})
	if err != nil {
		return err
	}
	defer output.Body.Close()

	if err := os.MkdirAll(filepath.Dir(item.Path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(item.Path), ".download-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if _, err := io.Copy(temp, output.Body); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}

	mode := os.FileMode(0o644)
	if isExecutableName(filepath.Base(item.Path)) {
		mode = 0o755
	}
	if err := os.Chmod(tempPath, mode); err != nil {
		return err
	}
	return os.Rename(tempPath, item.Path)
}

func localSize(path string) (int64, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0, false
	}
	return info.Size(), true
}
