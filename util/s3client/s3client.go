// Copyright 2024-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package s3client

import (
	"context"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/pflag"
)

// Config holds the base S3 connection configuration.
type Config struct {
	AccessKey string `koanf:"access-key"`
	SecretKey string `koanf:"secret-key"`
	Region    string `koanf:"region"`
	Endpoint  string `koanf:"endpoint"`
	// Nil preserves path-style addressing for custom endpoints. Ignored without an endpoint.
	UsePathStyle *bool `koanf:"use-path-style"`
}

var DefaultConfig = Config{}

func ConfigAddOptions(prefix string, f *pflag.FlagSet) {
	f.String(prefix+".access-key", DefaultConfig.AccessKey, "S3 access key")
	f.String(prefix+".secret-key", DefaultConfig.SecretKey, "S3 secret key")
	f.String(prefix+".region", DefaultConfig.Region, "S3 region")
	f.String(prefix+".endpoint", DefaultConfig.Endpoint, "custom S3 endpoint URL (for MinIO, localstack, or other S3-compatible services)")
	f.Bool(prefix+".use-path-style", true, "use path-style addressing with a custom S3 endpoint; set false for virtual-hosted addressing (e.g. Tencent COS); ignored without a custom endpoint")
}

func NewS3FullClientFromConfig(ctx context.Context, config *Config) (FullClient, error) {
	usePathStyle := true
	if config.UsePathStyle != nil {
		usePathStyle = *config.UsePathStyle
	}
	return newS3FullClient(ctx, config.AccessKey, config.SecretKey, config.Region, config.Endpoint, usePathStyle)
}

type Uploader interface {
	Upload(ctx context.Context, input *s3.PutObjectInput, opts ...func(*manager.Uploader)) (*manager.UploadOutput, error)
}

type Downloader interface {
	Download(ctx context.Context, w io.WriterAt, input *s3.GetObjectInput, options ...func(*manager.Downloader)) (n int64, err error)
}

type FullClient interface {
	Uploader
	Downloader
	Client() *s3.Client
}

type s3Client struct {
	client     *s3.Client
	uploader   Uploader
	downloader Downloader
}

func NewS3FullClient(ctx context.Context, accessKey, secretKey, region, endpoint string) (FullClient, error) {
	return newS3FullClient(ctx, accessKey, secretKey, region, endpoint, true)
}

func newS3FullClient(ctx context.Context, accessKey, secretKey, region, endpoint string, usePathStyle bool) (FullClient, error) {
	cfg, err := awsConfig.LoadDefaultConfig(ctx, awsConfig.WithRegion(region), func(options *awsConfig.LoadOptions) error {
		// remain backward compatible with accessKey and secretKey credentials provided via cli flags
		if accessKey != "" && secretKey != "" {
			options.Credentials = credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var client *s3.Client
	if endpoint != "" {
		// Preserve path-style by default, but allow providers requiring virtual-hosted addressing.
		client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = usePathStyle
		})
	} else {
		client = s3.NewFromConfig(cfg)
	}
	return &s3Client{
		client:     client,
		uploader:   manager.NewUploader(client),
		downloader: manager.NewDownloader(client),
	}, nil
}

func (s *s3Client) Client() *s3.Client {
	return s.client
}

func (s *s3Client) Upload(ctx context.Context, input *s3.PutObjectInput, opts ...func(*manager.Uploader)) (*manager.UploadOutput, error) {
	return s.uploader.Upload(ctx, input, opts...)
}

func (s *s3Client) Download(ctx context.Context, w io.WriterAt, input *s3.GetObjectInput, options ...func(*manager.Downloader)) (n int64, err error) {
	return s.downloader.Download(ctx, w, input, options...)
}
