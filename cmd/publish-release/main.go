// Command publish-release 把发布产物上传到 S3 兼容存储（Cloudflare R2），
// 并发布应用内更新使用的 updates/latest.json。
//
// 上传顺序固定为「先资产、后清单」，保证客户端不会看到资产还没传完的版本；
// 只上传 release.json 清单里列出的文件。
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
		dir       = flag.String("dir", "dist", "发布产物所在目录")
		version   = flag.String("version", "", "版本号（可带 v 前缀；缺省时从 manifest 读取）")
		bucket    = flag.String("bucket", "uiautodev", "目标 bucket")
		prefix    = flag.String("prefix", "", "bucket 内的 key 前缀")
		manifest  = flag.String("manifest", "release.json", "同时作为最新清单发布的文件")
		latestKey = flag.String("latest-key", "updates/latest.json", "最新清单在 bucket 中的 key")
		guardURL  = flag.String("latest-guard-url", "", "线上最新清单地址，用于避免把线上版本回退")
		endpoint  = flag.String("endpoint", os.Getenv("S3_ENDPOINT"), "S3 endpoint，如 https://<account>.r2.cloudflarestorage.com")
		region    = flag.String("region", "auto", "S3 region")
		dryRun    = flag.Bool("dry-run", false, "只打印上传计划，不实际上传")
	)
	flag.Parse()

	manifestPath := filepath.Join(*dir, *manifest)
	release, err := readManifest(manifestPath)
	if err != nil {
		log.Fatalf("读取清单 %s 失败: %v", manifestPath, err)
	}

	resolvedVersion := strings.TrimPrefix(strings.TrimSpace(*version), "v")
	if resolvedVersion == "" {
		resolvedVersion = strings.TrimPrefix(strings.TrimSpace(release.Version), "v")
	}
	if resolvedVersion == "" {
		log.Fatal("无法确定版本号：-version 未指定，且清单里没有 version")
	}

	items, err := planUploads(release, *dir, *prefix, resolvedVersion, *manifest)
	if err != nil {
		log.Fatal(err)
	}
	if len(items) == 0 {
		log.Fatalf("%s 里没有可上传的产物", *dir)
	}

	var total int64
	for _, item := range items {
		total += item.Size
	}
	log.Printf("要上传 %d 个文件（%.1f MiB）到 s3://%s/%s%s/",
		len(items), float64(total)/(1<<20), *bucket, *prefix, resolvedVersion)
	for _, item := range items {
		log.Printf("  %s -> %s", filepath.Base(item.Path), item.Key)
	}
	log.Printf("  然后发布 %s -> s3://%s/%s", *manifest, *bucket, *latestKey)

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
			log.Fatalf("上传 %s 失败: %v", item.Key, err)
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
		log.Fatalf("发布 %s 失败: %v", *latestKey, err)
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
