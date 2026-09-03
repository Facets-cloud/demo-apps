// Package signer mints V4 signed PUT URLs for direct-to-GCS uploads. It
// defines the URLSigner seam, a GCS-backed implementation, and an in-memory
// Fake for tests.
package signer

import (
	"context"
	"fmt"
	"sync"
	"time"

	credentials "cloud.google.com/go/iam/credentials/apiv1"
	"cloud.google.com/go/iam/credentials/apiv1/credentialspb"
	"cloud.google.com/go/storage"
)

// URLSigner produces signed URLs for direct-to-GCS access: SignPut for the
// browser to upload the receipt bytes, SignGet for the browser to view them.
type URLSigner interface {
	SignPut(ctx context.Context, object, contentType string, ttl time.Duration) (string, error)
	SignGet(ctx context.Context, object string, ttl time.Duration) (string, error)
}

// Call records a single SignPut invocation for the Fake.
type Call struct {
	Object      string
	ContentType string
	TTL         time.Duration
}

// Fake is an in-memory URLSigner for tests and local runs. It returns a
// predictable pseudo-URL that references the object.
type Fake struct {
	mu    sync.Mutex
	Calls []Call
	Err   error
}

// NewFake returns a ready-to-use fake signer.
func NewFake() *Fake { return &Fake{} }

// SignPut records the call and returns a deterministic fake URL.
func (f *Fake) SignPut(_ context.Context, object, contentType string, ttl time.Duration) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Object: object, ContentType: contentType, TTL: ttl})
	return fmt.Sprintf("https://storage.example/fake-signed/%s?ct=%s", object, contentType), nil
}

// SignGet records the call and returns a deterministic fake download URL.
func (f *Fake) SignGet(_ context.Context, object string, ttl time.Duration) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Object: object, TTL: ttl})
	return fmt.Sprintf("https://storage.example/fake-signed-get/%s", object), nil
}

// GCSSigner is the real V4 signer. In Cloud Functions the runtime service
// account signs the URL via the IAM Credentials SignBlob API, so no private
// key file is needed on disk.
//
// Deploy note: the runtime service account (SignerSA) must hold
// iam.serviceAccounts.signBlob on itself (roles/iam.serviceAccountTokenCreator).
type GCSSigner struct {
	bucket   string
	signerSA string // service account email used as GoogleAccessID
	iam      *credentials.IamCredentialsClient
}

// NewGCSSigner builds a signer for the bucket using the given signer SA email.
func NewGCSSigner(ctx context.Context, bucket, signerSA string) (*GCSSigner, error) {
	iam, err := credentials.NewIamCredentialsClient(ctx)
	if err != nil {
		return nil, err
	}
	return &GCSSigner{bucket: bucket, signerSA: signerSA, iam: iam}, nil
}

// SignPut mints a V4 signed URL for an HTTP PUT of the given content type.
func (g *GCSSigner) SignPut(ctx context.Context, object, contentType string, ttl time.Duration) (string, error) {
	opts := &storage.SignedURLOptions{
		Scheme:         storage.SigningSchemeV4,
		Method:         "PUT",
		Expires:        time.Now().Add(ttl),
		ContentType:    contentType,
		GoogleAccessID: g.signerSA,
		SignBytes: func(b []byte) ([]byte, error) {
			resp, err := g.iam.SignBlob(ctx, &credentialspb.SignBlobRequest{
				Name:    "projects/-/serviceAccounts/" + g.signerSA,
				Payload: b,
			})
			if err != nil {
				return nil, err
			}
			return resp.SignedBlob, nil
		},
	}
	return storage.SignedURL(g.bucket, object, opts)
}

// SignGet mints a V4 signed URL for an HTTP GET, so the browser can view a
// private object without the bucket being public.
func (g *GCSSigner) SignGet(ctx context.Context, object string, ttl time.Duration) (string, error) {
	opts := &storage.SignedURLOptions{
		Scheme:         storage.SigningSchemeV4,
		Method:         "GET",
		Expires:        time.Now().Add(ttl),
		GoogleAccessID: g.signerSA,
		SignBytes: func(b []byte) ([]byte, error) {
			resp, err := g.iam.SignBlob(ctx, &credentialspb.SignBlobRequest{
				Name:    "projects/-/serviceAccounts/" + g.signerSA,
				Payload: b,
			})
			if err != nil {
				return nil, err
			}
			return resp.SignedBlob, nil
		},
	}
	return storage.SignedURL(g.bucket, object, opts)
}

// Close releases the IAM client.
func (g *GCSSigner) Close() error { return g.iam.Close() }
