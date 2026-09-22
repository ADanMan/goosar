package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Storage struct {
	client       *s3.Client
	bucket       string
	region       string
	cdnDomain    string
	endpointURL  string
	usePathStyle bool
}

func ValidateS3Env() error {
	bucket := strings.TrimSpace(os.Getenv("S3_BUCKET"))
	if bucket == "" {
		return nil
	}
	endpointURL := strings.TrimSpace(os.Getenv("AWS_ENDPOINT_URL"))
	region := strings.TrimSpace(os.Getenv("S3_REGION"))
	if endpointURL == "" && region == "" {
		return errors.New("S3_BUCKET is set but no storage destination is configured — refusing to silently upload to AWS S3 us-west-2. Set AWS_ENDPOINT_URL for an S3-compatible endpoint (MinIO, R2, B2, ...), or set S3_REGION explicitly to confirm AWS S3 as the target")
	}
	return nil
}

func NewS3StorageFromEnv() *S3Storage {
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		slog.Info("S3_BUCKET not set, cloud upload disabled")
		return nil
	}
	if err := ValidateS3Env(); err != nil {
		slog.Error("S3 storage misconfigured — cloud upload disabled", "error", err)
		return nil
	}
	if looksLikeS3Hostname(bucket) {
		slog.Warn(
			"S3_BUCKET looks like a hostname rather than a bucket name — uploads and public URLs will likely both fail. Use only the bucket name (e.g. \"my-bucket\"), not \"<bucket>.s3.<region>.amazonaws.com\".",
			"value", bucket,
		)
	}

	region := strings.TrimSpace(os.Getenv("S3_REGION"))

	opts := []func(*config.LoadOptions) error{}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKey != "" && secretKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		slog.Error("failed to load AWS config", "error", err)
		return nil
	}

	cdnDomain := os.Getenv("CLOUDFRONT_DOMAIN")

	endpointURL := os.Getenv("AWS_ENDPOINT_URL")
	usePathStyle := s3UsePathStyleFromEnv(endpointURL)
	s3Opts := []func(*s3.Options){}
	if endpointURL != "" || usePathStyle {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			if endpointURL != "" {
				o.BaseEndpoint = aws.String(endpointURL)
			}
			o.UsePathStyle = usePathStyle
		})
	}

	slog.Info("S3 storage initialized", "bucket", bucket, "region", region, "cdn_domain", cdnDomain, "endpoint_url", endpointURL, "use_path_style", usePathStyle)
	return &S3Storage{
		client:       s3.NewFromConfig(cfg, s3Opts...),
		bucket:       bucket,
		region:       region,
		cdnDomain:    cdnDomain,
		endpointURL:  endpointURL,
		usePathStyle: usePathStyle,
	}
}

func (s *S3Storage) CdnDomain() string {
	return s.cdnDomain
}

func looksLikeS3Hostname(bucket string) bool {
	return strings.Contains(bucket, "amazonaws.com")
}

func s3UsePathStyleFromEnv(endpointURL string) bool {
	defaultValue := endpointURL != ""
	raw, ok := os.LookupEnv("S3_USE_PATH_STYLE")
	if !ok || strings.TrimSpace(raw) == "" {
		return defaultValue
	}
	parsed, err := parseBoolEnv(raw)
	if err != nil {
		slog.Warn("invalid S3_USE_PATH_STYLE value, using default", "value", raw, "default", defaultValue)
		return defaultValue
	}
	return parsed
}

func parseBoolEnv(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool %q", raw)
	}
}

func (s *S3Storage) storageClass() types.StorageClass {
	if s.endpointURL != "" {
		return types.StorageClassStandard
	}
	return types.StorageClassIntelligentTiering
}

func (s *S3Storage) KeyFromURL(rawURL string) string {
	if s.endpointURL != "" {
		for _, prefix := range []string{
			customEndpointObjectPrefix(s.endpointURL, s.bucket, true),
			customEndpointObjectPrefix(s.endpointURL, s.bucket, false),
		} {
			if strings.HasPrefix(rawURL, prefix) {
				return strings.TrimPrefix(rawURL, prefix)
			}
		}
	}

	prefixes := make([]string, 0, 5)
	if s.cdnDomain != "" {
		prefixes = append(prefixes, "https://"+s.cdnDomain+"/")
	}
	if s.region != "" {

		prefixes = append(prefixes,
			"https://"+s.bucket+".s3."+s.region+".amazonaws.com/",

			"https://s3."+s.region+".amazonaws.com/"+s.bucket+"/",
		)
	}

	prefixes = append(prefixes, "https://"+s.bucket+"/")

	for _, prefix := range prefixes {
		if strings.HasPrefix(rawURL, prefix) {
			return strings.TrimPrefix(rawURL, prefix)
		}
	}

	if i := strings.LastIndex(rawURL, "/"); i >= 0 {
		return rawURL[i+1:]
	}
	return rawURL
}

func (s *S3Storage) GetReader(ctx context.Context, key string) (io.ReadCloser, error) {
	if key == "" {
		return nil, fmt.Errorf("s3 GetReader: empty key")
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 GetObject: %w", err)
	}
	return out.Body, nil
}

func (s *S3Storage) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return s.PresignGetWithContentDisposition(ctx, key, ttl, "")
}

func (s *S3Storage) PresignGetWithContentDisposition(ctx context.Context, key string, ttl time.Duration, contentDisposition string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("s3 PresignGet: empty key")
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	input := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	if contentDisposition != "" {
		input.ResponseContentDisposition = aws.String(contentDisposition)
	}
	out, err := s3.NewPresignClient(s.client).PresignGetObject(ctx, input, func(opts *s3.PresignOptions) {
		opts.Expires = ttl
	})
	if err != nil {
		return "", fmt.Errorf("s3 PresignGetObject: %w", err)
	}
	return out.URL, nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) {
	if err := s.DeleteObject(ctx, key); err != nil {
		slog.Error("s3 DeleteObject failed", "key", key, "error", err)
	}
}

func (s *S3Storage) DeleteObject(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	return err
}

func (s *S3Storage) ObjectURL(key string) string {
	return s.uploadedURL(key)
}

func (s *S3Storage) DeleteKeys(ctx context.Context, keys []string) {
	for _, key := range keys {
		s.Delete(ctx, key)
	}
}

func (s *S3Storage) Upload(ctx context.Context, key string, data []byte, contentType string, filename string) (string, error) {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:             aws.String(s.bucket),
		Key:                aws.String(key),
		Body:               bytes.NewReader(data),
		ContentType:        aws.String(contentType),
		ContentDisposition: aws.String(ContentDisposition(contentType, filename)),
		CacheControl:       aws.String("max-age=432000,public"),
		StorageClass:       s.storageClass(),
	})
	if err != nil {
		return "", fmt.Errorf("s3 PutObject: %w", err)
	}
	return s.uploadedURL(key), nil
}

func (s *S3Storage) UploadStream(ctx context.Context, key string, data io.Reader, sizeBytes int64, contentType string, filename string) (string, error) {
	if sizeBytes <= 0 {
		return "", fmt.Errorf("s3 PutObject: content length is required for streaming upload")
	}
	input := &s3.PutObjectInput{
		Bucket:             aws.String(s.bucket),
		Key:                aws.String(key),
		Body:               data,
		ContentLength:      aws.Int64(sizeBytes),
		ContentType:        aws.String(contentType),
		ContentDisposition: aws.String(ContentDisposition(contentType, filename)),
		CacheControl:       aws.String("max-age=432000,public"),
		StorageClass:       s.storageClass(),
	}
	_, err := s.client.PutObject(ctx, input, func(opts *s3.Options) {

		opts.APIOptions = append(opts.APIOptions, v4.SwapComputePayloadSHA256ForUnsignedPayloadMiddleware)
		opts.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
	if err != nil {
		return "", fmt.Errorf("s3 PutObject: %w", err)
	}
	return s.uploadedURL(key), nil
}

func (s *S3Storage) uploadedURL(key string) string {
	if s.cdnDomain != "" {
		return fmt.Sprintf("https://%s/%s", s.cdnDomain, key)
	}
	if s.endpointURL != "" {
		return customEndpointObjectURL(s.endpointURL, s.bucket, key, s.usePathStyle)
	}
	if s.usePathStyle || strings.Contains(s.bucket, ".") {
		return fmt.Sprintf("https://s3.%s.amazonaws.com/%s/%s", s.region, s.bucket, key)
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.bucket, s.region, key)
}

func customEndpointObjectURL(endpointURL, bucket, key string, usePathStyle bool) string {
	return customEndpointObjectPrefix(endpointURL, bucket, usePathStyle) + key
}

func customEndpointObjectPrefix(endpointURL, bucket string, usePathStyle bool) string {
	trimmed := strings.TrimRight(endpointURL, "/")
	if usePathStyle {
		return trimmed + "/" + bucket + "/"
	}

	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return trimmed + "/"
	}
	u.Host = bucket + "." + u.Host
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
