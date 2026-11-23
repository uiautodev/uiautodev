// Package s3store 提供 S3 兼容存储（Cloudflare R2、MinIO 等）的连接配置。
package s3store

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Settings 描述目标存储的连接信息。
type Settings struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
}

// Load 按优先级解析配置：显式参数 > S3_* / R2_* 环境变量 > rclone.conf 的 r2 remote。
func Load(endpoint, region string) (Settings, error) {
	settings := Settings{
		Endpoint:        strings.TrimSpace(endpoint),
		Region:          strings.TrimSpace(region),
		AccessKeyID:     firstEnv("S3_ACCESS_KEY_ID", "R2_ACCESS_KEY_ID", "AWS_ACCESS_KEY_ID"),
		SecretAccessKey: firstEnv("S3_SECRET_ACCESS_KEY", "R2_SECRET_ACCESS_KEY", "AWS_SECRET_ACCESS_KEY"),
	}

	if settings.Endpoint == "" {
		settings.Endpoint = firstEnv("S3_ENDPOINT", "R2_ENDPOINT")
	}

	if settings.Endpoint == "" || settings.AccessKeyID == "" || settings.SecretAccessKey == "" {
		remote, err := readRcloneRemote("r2")
		if err == nil {
			if settings.Endpoint == "" {
				settings.Endpoint = remote["endpoint"]
			}
			if settings.AccessKeyID == "" {
				settings.AccessKeyID = remote["access_key_id"]
			}
			if settings.SecretAccessKey == "" {
				settings.SecretAccessKey = remote["secret_access_key"]
			}
		}
	}

	if settings.Region == "" {
		settings.Region = "auto"
	}
	if settings.Endpoint == "" {
		return Settings{}, fmt.Errorf("缺少 S3 endpoint：请用 -endpoint 或 S3_ENDPOINT 指定，或在 rclone.conf 里配置 r2 remote")
	}
	if settings.AccessKeyID == "" || settings.SecretAccessKey == "" {
		return Settings{}, fmt.Errorf("缺少访问密钥：请设置 S3_ACCESS_KEY_ID / S3_SECRET_ACCESS_KEY（或 R2_* 同名变量）")
	}
	return settings, nil
}

// Client 创建使用 path-style 寻址的 S3 客户端（R2 要求）。
func (s Settings) Client() (*s3.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(s.Region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(s.AccessKeyID, s.SecretAccessKey, ""),
		),
	)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = aws.String(s.Endpoint)
	}), nil
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

// readRcloneRemote 读取 rclone.conf 里指定 remote 的键值，用于本地免配置。
func readRcloneRemote(name string) (map[string]string, error) {
	path := filepath.Join(homeDir(), ".config", "rclone", "rclone.conf")
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := map[string]string{}
	inSection := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inSection = strings.TrimSpace(line[1:len(line)-1]) == name
			continue
		}
		if !inSection {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("rclone.conf 里没有 [%s]", name)
	}
	return values, nil
}

func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "."
}
