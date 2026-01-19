package adapters

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Adapter uploads files to S3 or S3-compatible storage
type S3Adapter struct {
	client     *s3.Client
	bucket     string
	pathPrefix string
	publicURL  string
	region     string
	endpoint   string
}

// NewS3Adapter creates a new S3 adapter from environment variables
func NewS3Adapter() (*S3Adapter, error) {
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		return nil, fmt.Errorf("S3_BUCKET is required")
	}

	accessKey := os.Getenv("S3_ACCESS_KEY")
	if accessKey == "" {
		return nil, fmt.Errorf("S3_ACCESS_KEY is required")
	}

	secretKey := os.Getenv("S3_SECRET_KEY")
	if secretKey == "" {
		return nil, fmt.Errorf("S3_SECRET_KEY is required")
	}

	region := getEnv("S3_REGION", "us-east-1")
	endpoint := os.Getenv("S3_ENDPOINT")
	pathPrefix := os.Getenv("S3_PATH_PREFIX")
	publicURL := os.Getenv("S3_PUBLIC_URL")

	// Build AWS config
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	// Create S3 client with optional custom endpoint
	var client *s3.Client
	if endpoint != "" {
		client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true // Required for most S3-compatible services
		})
	} else {
		client = s3.NewFromConfig(cfg)
	}

	return &S3Adapter{
		client:     client,
		bucket:     bucket,
		pathPrefix: pathPrefix,
		publicURL:  publicURL,
		region:     region,
		endpoint:   endpoint,
	}, nil
}

func (a *S3Adapter) Name() string {
	return "s3"
}

func (a *S3Adapter) Upload(ctx context.Context, localPath string, destPath string) (string, error) {
	// Build S3 key with optional prefix
	key := destPath
	if a.pathPrefix != "" {
		key = strings.TrimSuffix(a.pathPrefix, "/") + "/" + destPath
	}

	f, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	contentType := getContentType(localPath)

	_, err = a.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(a.bucket),
		Key:         aws.String(key),
		Body:        f,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("upload to s3: %w", err)
	}

	log.Printf("[s3] Uploaded %s -> s3://%s/%s", localPath, a.bucket, key)

	// Return public URL if configured
	if a.publicURL != "" {
		return strings.TrimSuffix(a.publicURL, "/") + "/" + key, nil
	}

	// Return S3 URL
	if a.endpoint != "" {
		return fmt.Sprintf("%s/%s/%s", strings.TrimSuffix(a.endpoint, "/"), a.bucket, key), nil
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", a.bucket, a.region, key), nil
}
