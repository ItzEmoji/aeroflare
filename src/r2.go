package network

import (
	"bytes"
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

type R2Config struct {
	Bucket    string
	Endpoint  string
	AccessKey string
	SecretKey string
	PublicURL string
}

func GetR2Config(annotations map[string]string) *R2Config {
	bucket := os.Getenv("R2_BUCKET")
	if bucket == "" && annotations != nil {
		bucket = annotations["aeroflare.r2.bucket"]
	}

	if bucket == "" {
		return nil
	}

	endpoint := os.Getenv("R2_ENDPOINT")
	if endpoint == "" && annotations != nil {
		endpoint = annotations["aeroflare.r2.endpoint"]
	}

	accessKey := os.Getenv("R2_ACCESS_KEY_ID")
	secretKey := os.Getenv("R2_SECRET_ACCESS_KEY")

	publicURL := os.Getenv("R2_PUBLIC_URL")
	if publicURL == "" && annotations != nil {
		if u := annotations["public-r2-url"]; u != "" {
			publicURL = u
		} else if u := annotations["aeroflare.r2.public_url"]; u != "" {
			publicURL = u
		}
	}

	return &R2Config{
		Bucket:    bucket,
		Endpoint:  endpoint,
		AccessKey: accessKey,
		SecretKey: secretKey,
		PublicURL: publicURL,
	}
}

func (r *R2Config) NewClient(ctx context.Context) (*s3.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(r.AccessKey, r.SecretKey, "")),
		config.WithRegion("auto"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load R2 config: %w", err)
	}

	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(r.Endpoint)
		o.UsePathStyle = true
	}), nil
}

func (r *R2Config) UploadNarinfo(ctx context.Context, client *s3.Client, storePath, narinfoPath string) error {
	b, err := os.ReadFile(narinfoPath)
	if err != nil {
		return fmt.Errorf("read narinfo: %w", err)
	}

	basename := filepath.Base(storePath)
	parts := strings.SplitN(basename, "-", 2)
	if len(parts) < 2 {
		return fmt.Errorf("invalid store path format: %s", basename)
	}
	hash := parts[0]
	objectKey := fmt.Sprintf("%s.narinfo", hash)

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(r.Bucket),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(b),
		ContentType: aws.String("text/x-nix-narinfo"),
	})

	if err != nil {
		return fmt.Errorf("upload to R2: %w", err)
	}

	return nil
}
