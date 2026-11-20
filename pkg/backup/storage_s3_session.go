// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

//go:build awss3

package backup

import (
	"context"
	"io"
	"net/http"
	"net/url"

	"github.com/aws/aws-sdk-go/aws"                  //nolint:staticcheck // go-objstore's S3 backend is on the v1 SDK
	"github.com/aws/aws-sdk-go/aws/awserr"           //nolint:staticcheck // go-objstore's S3 backend is on the v1 SDK
	"github.com/aws/aws-sdk-go/aws/credentials"      //nolint:staticcheck // go-objstore's S3 backend is on the v1 SDK
	"github.com/aws/aws-sdk-go/aws/session"          //nolint:staticcheck // go-objstore's S3 backend is on the v1 SDK
	"github.com/aws/aws-sdk-go/service/s3"           //nolint:staticcheck // go-objstore's S3 backend is on the v1 SDK
	"github.com/aws/aws-sdk-go/service/s3/s3iface"   //nolint:staticcheck // go-objstore's S3 backend is on the v1 SDK
	"github.com/aws/aws-sdk-go/service/s3/s3manager" //nolint:staticcheck // go-objstore's S3 backend is on the v1 SDK
	"github.com/jeremyhahn/go-objstore/pkg/common"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

func init() {
	newSessionTokenS3 = newSessionS3
}

// sessionS3 is the S3 backend used when the configuration carries temporary
// credentials (session_token). go-objstore v0.3.0-alpha's S3 backend builds
// its credentials as NewStaticCredentials(accessKey, secretKey, "") and so
// drops the token, which S3 then rejects; this backend signs every request
// with the full access key, secret key and session token tuple.
type sessionS3 struct {
	svc    s3iface.S3API
	bucket string
}

func newSessionS3(settings map[string]string) (common.Storage, error) {
	s := &sessionS3{}
	if err := s.Configure(settings); err != nil {
		return nil, err
	}
	return s, nil
}

// Configure reads go-objstore's S3 keys (bucket, region, endpoint,
// forcePathStyle, accessKey, secretKey) and sessionToken.
func (s *sessionS3) Configure(settings map[string]string) error {
	s.bucket = settings["bucket"]
	if s.bucket == "" {
		return common.ErrBucketNotSet
	}
	ak, sk := settings["accessKey"], settings["secretKey"]
	if ak == "" {
		return common.ErrAccessKeyNotSet
	}
	if sk == "" {
		return common.ErrSecretKeyNotSet
	}
	cfg := &aws.Config{
		Region:      aws.String(settings["region"]),
		Credentials: credentials.NewStaticCredentials(ak, sk, settings["sessionToken"]),
	}
	if ep := settings["endpoint"]; ep != "" {
		cfg.Endpoint = aws.String(ep)
		cfg.S3ForcePathStyle = aws.Bool(settings["forcePathStyle"] == "true")
	}
	sess, err := session.NewSession(cfg)
	if err != nil {
		return api.NewOperationFailedError("create S3 session", err)
	}
	s.svc = s3.New(sess)
	return nil
}

func (s *sessionS3) Put(key string, data io.Reader) error {
	return s.PutWithMetadata(context.Background(), key, data, nil)
}

func (s *sessionS3) PutWithContext(ctx context.Context, key string, data io.Reader) error {
	return s.PutWithMetadata(ctx, key, data, nil)
}

func (s *sessionS3) PutWithMetadata(ctx context.Context, key string, data io.Reader, md *common.Metadata) error {
	if err := common.ValidateKey(key); err != nil {
		return err
	}
	in := &s3manager.UploadInput{Bucket: aws.String(s.bucket), Key: aws.String(key), Body: data}
	if md != nil {
		if md.ContentType != "" {
			in.ContentType = aws.String(md.ContentType)
		}
		if md.ContentEncoding != "" {
			in.ContentEncoding = aws.String(md.ContentEncoding)
		}
		in.Metadata = aws.StringMap(md.Custom)
	}
	_, err := s3manager.NewUploaderWithClient(s.svc).UploadWithContext(ctx, in)
	return err
}

func (s *sessionS3) Get(key string) (io.ReadCloser, error) {
	return s.GetWithContext(context.Background(), key)
}

func (s *sessionS3) GetWithContext(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.svc.GetObjectWithContext(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

func (s *sessionS3) GetMetadata(ctx context.Context, key string) (*common.Metadata, error) {
	out, err := s.svc.HeadObjectWithContext(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	return &common.Metadata{
		ContentType:     aws.StringValue(out.ContentType),
		ContentEncoding: aws.StringValue(out.ContentEncoding),
		Size:            aws.Int64Value(out.ContentLength),
		LastModified:    aws.TimeValue(out.LastModified),
		ETag:            aws.StringValue(out.ETag),
		Custom:          aws.StringValueMap(out.Metadata),
	}, nil
}

func (s *sessionS3) UpdateMetadata(ctx context.Context, key string, md *common.Metadata) error {
	in := &s3.CopyObjectInput{
		Bucket:            aws.String(s.bucket),
		Key:               aws.String(key),
		CopySource:        aws.String(url.PathEscape(s.bucket + "/" + key)),
		MetadataDirective: aws.String(s3.MetadataDirectiveReplace),
	}
	if md != nil {
		if md.ContentType != "" {
			in.ContentType = aws.String(md.ContentType)
		}
		if md.ContentEncoding != "" {
			in.ContentEncoding = aws.String(md.ContentEncoding)
		}
		in.Metadata = aws.StringMap(md.Custom)
	}
	_, err := s.svc.CopyObjectWithContext(ctx, in)
	return err
}

func (s *sessionS3) Delete(key string) error {
	return s.DeleteWithContext(context.Background(), key)
}

func (s *sessionS3) DeleteWithContext(ctx context.Context, key string) error {
	_, err := s.svc.DeleteObjectWithContext(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

func (s *sessionS3) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.svc.HeadObjectWithContext(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err == nil {
		return true, nil
	}
	if rf, ok := err.(awserr.RequestFailure); ok && rf.StatusCode() == http.StatusNotFound {
		return false, nil
	}
	return false, err
}

func (s *sessionS3) List(prefix string) ([]string, error) {
	return s.ListWithContext(context.Background(), prefix)
}

func (s *sessionS3) ListWithContext(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	err := s.svc.ListObjectsV2PagesWithContext(ctx,
		&s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix)},
		func(page *s3.ListObjectsV2Output, _ bool) bool {
			for _, o := range page.Contents {
				keys = append(keys, aws.StringValue(o.Key))
			}
			return true
		})
	return keys, err
}

func (s *sessionS3) ListWithOptions(ctx context.Context, opts *common.ListOptions) (*common.ListResult, error) {
	in := &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket)}
	if opts != nil {
		if opts.Prefix != "" {
			in.Prefix = aws.String(opts.Prefix)
		}
		if opts.Delimiter != "" {
			in.Delimiter = aws.String(opts.Delimiter)
		}
		if opts.MaxResults > 0 {
			in.MaxKeys = aws.Int64(int64(opts.MaxResults))
		}
		if opts.ContinueFrom != "" {
			in.ContinuationToken = aws.String(opts.ContinueFrom)
		}
	}
	out, err := s.svc.ListObjectsV2WithContext(ctx, in)
	if err != nil {
		return nil, err
	}
	res := &common.ListResult{
		NextToken: aws.StringValue(out.NextContinuationToken),
		Truncated: aws.BoolValue(out.IsTruncated),
	}
	for _, o := range out.Contents {
		res.Objects = append(res.Objects, &common.ObjectInfo{
			Key: aws.StringValue(o.Key),
			Metadata: &common.Metadata{
				Size:         aws.Int64Value(o.Size),
				LastModified: aws.TimeValue(o.LastModified),
				ETag:         aws.StringValue(o.ETag),
			},
		})
	}
	for _, p := range out.CommonPrefixes {
		res.CommonPrefixes = append(res.CommonPrefixes, aws.StringValue(p.Prefix))
	}
	return res, nil
}

func (s *sessionS3) Archive(key string, destination common.Archiver) error {
	rc, err := s.Get(key)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	return destination.Put(key, rc)
}

// AddPolicy is refused: lifecycle policies are bucket configuration, which
// the backup manager does not use and this backend does not manage.
func (s *sessionS3) AddPolicy(common.LifecyclePolicy) error {
	return api.NewUnsupportedValueError("backup.sessionS3.AddPolicy", "lifecycle policy",
		"session-token S3 backend")
}

// RemovePolicy is refused, as AddPolicy is.
func (s *sessionS3) RemovePolicy(string) error {
	return api.NewUnsupportedValueError("backup.sessionS3.RemovePolicy", "lifecycle policy",
		"session-token S3 backend")
}

// GetPolicies reports no policies: this backend never adds one.
func (s *sessionS3) GetPolicies() ([]common.LifecyclePolicy, error) {
	return nil, nil
}
