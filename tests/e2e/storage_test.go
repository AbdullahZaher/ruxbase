package e2e_test

import (
	"context"
	"errors"
	"path"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/types"
)

func TestStorage_BucketCreationAndQuotaEnforcement(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create bucket with 50KB size limit and restricted MIME types
	bucket := &core.StorageBucket{
		Id:            "bkt_quota_test",
		Name:          "avatars",
		Public:        false,
		FileSizeLimit: 50 * 1024, // 50KB limit
		AllowedMimeTypes: types.JSONArray[string]{
			"image/png",
			"image/jpeg",
		},
	}
	createdBucket, err := app.Storage().CreateBucket(bucket)
	if err != nil {
		t.Fatalf("Failed to create bucket: %v", err)
	}
	if createdBucket.Name != "avatars" {
		t.Fatalf("Expected bucket name avatars, got %s", createdBucket.Name)
	}

	// 2. Reject upload exceeding file_size_limit
	largePayload := make([]byte, 60*1024) // 60KB > 50KB
	_, err = app.Storage().UploadObject(
		context.Background(),
		"avatars",
		"large_avatar.png",
		"user_01",
		"image/png",
		largePayload,
		nil,
	)
	if !errors.Is(err, core.ErrStorageFileSizeExceeded) {
		t.Fatalf("Expected ErrStorageFileSizeExceeded, got: %v", err)
	}

	// 3. Reject upload with unapproved MIME type
	textPayload := []byte("plain text not allowed in avatar bucket")
	_, err = app.Storage().UploadObject(
		context.Background(),
		"avatars",
		"notes.txt",
		"user_01",
		"text/plain",
		textPayload,
		nil,
	)
	if !errors.Is(err, core.ErrStorageInvalidMimeType) {
		t.Fatalf("Expected ErrStorageInvalidMimeType, got: %v", err)
	}

	// 4. Valid upload within limits and allowed MIME type
	validPayload := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDRsample_png_bytes")
	obj, err := app.Storage().UploadObject(
		context.Background(),
		"avatars",
		"user_01_avatar.png",
		"user_01",
		"image/png",
		validPayload,
		map[string]any{"source": "profile_upload"},
	)
	if err != nil {
		t.Fatalf("Valid upload failed: %v", err)
	}
	if obj.Size != int64(len(validPayload)) || obj.Etag == "" {
		t.Fatalf("Invalid stored object metadata: %+v", obj)
	}
}

func TestStorage_CrossTenantAuthorizationAndPrivateAccess(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create private bucket
	_, err = app.Storage().CreateBucket(&core.StorageBucket{
		Id:     "bkt_private_vault",
		Name:   "vault",
		Public: false,
	})
	if err != nil {
		t.Fatalf("Failed to create private bucket: %v", err)
	}

	// Upload object owned by user_alice
	aliceSecret := []byte("top_secret_tax_document_content")
	_, err = app.Storage().UploadObject(
		context.Background(),
		"vault",
		"taxes_2026.pdf",
		"user_alice",
		"application/pdf",
		aliceSecret,
		nil,
	)
	if err != nil {
		t.Fatalf("Alice upload failed: %v", err)
	}

	// 2. Cross-tenant read access denial: user_bob cannot read alice's private object
	_, _, err = app.Storage().GetObject(context.Background(), "vault", "taxes_2026.pdf", "user_bob", false)
	if !errors.Is(err, core.ErrStorageAccessDenied) {
		t.Fatalf("Cross-tenant read violation! Expected ErrStorageAccessDenied, got: %v", err)
	}

	// 3. Anonymous read access denial
	_, _, err = app.Storage().GetObject(context.Background(), "vault", "taxes_2026.pdf", "", false)
	if !errors.Is(err, core.ErrStorageAccessDenied) {
		t.Fatalf("Anonymous read violation on private bucket! Expected ErrStorageAccessDenied, got: %v", err)
	}

	// 4. Cross-tenant delete access denial: user_bob cannot delete alice's object
	err = app.Storage().DeleteObject(context.Background(), "vault", "taxes_2026.pdf", "user_bob", false)
	if !errors.Is(err, core.ErrStorageAccessDenied) {
		t.Fatalf("Cross-tenant delete violation! Expected ErrStorageAccessDenied, got: %v", err)
	}

	// 5. Authorized read by owner alice
	obj, data, err := app.Storage().GetObject(context.Background(), "vault", "taxes_2026.pdf", "user_alice", false)
	if err != nil {
		t.Fatalf("Alice read failed: %v", err)
	}
	if string(data) != string(aliceSecret) || obj.OwnerId != "user_alice" {
		t.Fatalf("Read data mismatch for owner: %s", string(data))
	}

	// 6. Superuser bypass access
	_, adminData, err := app.Storage().GetObject(context.Background(), "vault", "taxes_2026.pdf", "", true)
	if err != nil {
		t.Fatalf("Superuser read failed: %v", err)
	}
	if string(adminData) != string(aliceSecret) {
		t.Fatal("Superuser did not retrieve expected payload")
	}

	// 7. Public bucket anonymous read check
	_, err = app.Storage().CreateBucket(&core.StorageBucket{
		Id:     "bkt_public_assets",
		Name:   "assets",
		Public: true,
	})
	if err != nil {
		t.Fatalf("Failed to create public bucket: %v", err)
	}

	_, _ = app.Storage().UploadObject(context.Background(), "assets", "logo.svg", "admin", "image/svg+xml", []byte("<svg></svg>"), nil)
	_, publicData, err := app.Storage().GetObject(context.Background(), "assets", "logo.svg", "", false)
	if err != nil || string(publicData) != "<svg></svg>" {
		t.Fatalf("Anonymous read on public bucket failed: %v", err)
	}
}

func TestStorage_SignedURLTemporaryAccess(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	_, err = app.Storage().CreateBucket(&core.StorageBucket{
		Id:     "bkt_signed_test",
		Name:   "secure_reports",
		Public: false,
	})
	if err != nil {
		t.Fatalf("Failed to create bucket: %v", err)
	}

	content := []byte("confidential_financial_audit")
	_, err = app.Storage().UploadObject(context.Background(), "secure_reports", "audit.csv", "cfo", "text/csv", content, nil)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	secret := "test_cluster_signed_url_secret_key_123"

	// 1. Valid signed token with 10-minute TTL
	token, err := app.Storage().CreateSignedURL("secure_reports", "audit.csv", 10*time.Minute, secret)
	if err != nil {
		t.Fatalf("CreateSignedURL failed: %v", err)
	}

	bName, oName, err := app.Storage().VerifySignedToken(token, secret)
	if err != nil {
		t.Fatalf("VerifySignedToken failed on valid token: %v", err)
	}
	if bName != "secure_reports" || oName != "audit.csv" {
		t.Fatalf("Token verified to wrong target: %s / %s", bName, oName)
	}

	// 2. Reject expired token (TTL = -1 second)
	expiredToken, _ := app.Storage().CreateSignedURL("secure_reports", "audit.csv", -1*time.Second, secret)
	_, _, err = app.Storage().VerifySignedToken(expiredToken, secret)
	if !errors.Is(err, core.ErrStorageInvalidSignedToken) {
		t.Fatalf("Expected ErrStorageInvalidSignedToken for expired token, got: %v", err)
	}

	// 3. Reject tampered secret token
	_, _, err = app.Storage().VerifySignedToken(token, "tampered_wrong_secret")
	if !errors.Is(err, core.ErrStorageInvalidSignedToken) {
		t.Fatalf("Expected ErrStorageInvalidSignedToken for tampered token, got: %v", err)
	}
}

func TestStorage_OrphanReconciliation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	bucket, err := app.Storage().CreateBucket(&core.StorageBucket{
		Id:     "bkt_orphan_test",
		Name:   "orphan_bucket",
		Public: false,
	})
	if err != nil {
		t.Fatalf("Failed to create bucket: %v", err)
	}

	// 1. Upload valid tracked file
	_, err = app.Storage().UploadObject(context.Background(), "orphan_bucket", "legit.txt", "admin", "text/plain", []byte("legit"), nil)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	// 2. Plant orphan rogue file directly in filesystem
	fsys, err := app.NewFilesystem()
	if err != nil {
		t.Fatalf("Failed to get filesystem: %v", err)
	}
	orphanKey := path.Join("_storage", bucket.Id, "rogue_orphan.txt")
	if err := fsys.Upload([]byte("untracked leaked data"), orphanKey); err != nil {
		fsys.Close()
		t.Fatalf("Failed to plant orphan: %v", err)
	}
	fsys.Close()

	// 3. Reconcile orphans
	purged, err := app.Storage().ReconcileOrphans(context.Background(), "orphan_bucket")
	if err != nil {
		t.Fatalf("ReconcileOrphans error: %v", err)
	}
	if purged != 1 {
		t.Fatalf("Expected 1 orphan purged, got %d", purged)
	}

	// Verify orphan was deleted from filesystem
	fsys2, _ := app.NewFilesystem()
	defer fsys2.Close()
	exists, _ := fsys2.Exists(orphanKey)
	if exists {
		t.Fatal("Orphan file still exists on filesystem after reconciliation")
	}

	// Verify legitimate file was preserved
	legitKey := path.Join("_storage", bucket.Id, "legit.txt")
	legitExists, _ := fsys2.Exists(legitKey)
	if !legitExists {
		t.Fatal("Legitimate file was wrongly deleted during reconciliation")
	}
}
