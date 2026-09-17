// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package s3client

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/spf13/pflag"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestS3Addressing(t *testing.T) {
	// Never read developer credentials or attempt metadata/network credential discovery.
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/credentials")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ENDPOINT_URL", "")
	t.Setenv("AWS_ENDPOINT_URL_S3", "")

	for _, tc := range []struct {
		name         string
		endpoint     string
		usePathStyle *bool
		legacy       bool
		wantHost     string
		wantPath     string
	}{
		{"aws-default", "", nil, false, "test-bucket.s3.ap-southeast-1.amazonaws.com", "/"},
		{"aws-ignores-path-flag", "", aws.Bool(true), false, "test-bucket.s3.ap-southeast-1.amazonaws.com", "/"},
		{"aws-explicit-false", "", aws.Bool(false), false, "test-bucket.s3.ap-southeast-1.amazonaws.com", "/"},
		{"custom-default", "https://minio.example.com", nil, false, "minio.example.com", "/test-bucket"},
		{"custom-path", "https://minio.example.com", aws.Bool(true), false, "minio.example.com", "/test-bucket"},
		{"tencent-virtual", "https://cos.ap-singapore.myqcloud.com", aws.Bool(false), false, "test-bucket.cos.ap-singapore.myqcloud.com", "/"},
		{"legacy-constructor", "https://minio.example.com", nil, true, "minio.example.com", "/test-bucket"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := Config{
				AccessKey: "test-access-key", SecretKey: "test-secret-key",
				Region: "ap-southeast-1", Endpoint: tc.endpoint, UsePathStyle: tc.usePathStyle,
			}
			client, err := NewS3FullClientFromConfig(context.Background(), &config)
			if tc.legacy {
				client, err = NewS3FullClient(context.Background(), config.AccessKey, config.SecretKey, config.Region, config.Endpoint)
			}
			if err != nil {
				t.Fatal(err)
			}
			requests := 0
			_, err = client.Client().HeadBucket(context.Background(), &s3.HeadBucketInput{
				Bucket: aws.String("test-bucket"),
			}, func(o *s3.Options) {
				o.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					requests++
					if r.Method != http.MethodHead || r.URL.Host != tc.wantHost || r.URL.Path != tc.wantPath {
						t.Errorf("got %s %s, want HEAD https://%s%s", r.Method, r.URL, tc.wantHost, tc.wantPath)
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				})}
			})
			if err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Fatalf("got %d requests, want 1", requests)
			}
		})
	}
}

func TestUsePathStyleConfig(t *testing.T) {
	const prefix = "data-availability.s3-storage"
	for _, tc := range []struct {
		name string
		json string
		args []string
		want bool
	}{
		{"default", `{}`, nil, true},
		{"cli-false", `{}`, []string{"--" + prefix + ".use-path-style=false"}, false},
		{"json-false", `{"data-availability":{"s3-storage":{"use-path-style":false}}}`, nil, false},
		{"cli-overrides-json", `{"data-availability":{"s3-storage":{"use-path-style":false}}}`, []string{"--" + prefix + ".use-path-style=true"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			ConfigAddOptions(prefix, flags)
			if err := flags.Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			k := koanf.New(".")
			if err := k.Load(rawbytes.Provider([]byte(tc.json)), json.Parser()); err != nil {
				t.Fatal(err)
			}
			if err := k.Load(posflag.Provider(flags, ".", k), nil); err != nil {
				t.Fatal(err)
			}
			// Match the embedded S3 configuration used by DAS and other callers.
			var config struct {
				Config `koanf:",squash"`
			}
			if err := k.Unmarshal(prefix, &config); err != nil {
				t.Fatal(err)
			}
			if config.UsePathStyle == nil || *config.UsePathStyle != tc.want {
				t.Fatalf("got use-path-style %v, want %v", config.UsePathStyle, tc.want)
			}
		})
	}
}
