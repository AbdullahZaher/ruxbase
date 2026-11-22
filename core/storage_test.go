package core_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestStorageManager_MultipartUploadConcurrency(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-concurrency"
	_, err = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 50 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}

	session, err := sm.StartMultipartUpload(bucketName, "concurrent_object.bin", "tenant_test")
	if err != nil {
		t.Fatalf("failed to start multipart upload: %v", err)
	}

	const numParts = 10
	const partSize = 64 * 1024 // 64 KiB per part
	partData := make([][]byte, numParts)
	var expectedAssembled bytes.Buffer

	for i := 0; i < numParts; i++ {
		partData[i] = bytes.Repeat([]byte{byte('A' + i)}, partSize)
		expectedAssembled.Write(partData[i])
	}

	// Upload all 10 parts concurrently
	var wg sync.WaitGroup
	errCh := make(chan error, numParts)

	for i := 0; i < numParts; i++ {
		wg.Add(1)
		go func(partNum int) {
			defer wg.Done()
			_, upErr := sm.UploadPart(session.Id, partNum, partData[partNum-1])
			if upErr != nil {
				errCh <- fmt.Errorf("UploadPart %d failed: %w", partNum, upErr)
			}
		}(i + 1)
	}

	wg.Wait()
	close(errCh)

	for upErr := range errCh {
		t.Fatalf("concurrent upload error: %v", upErr)
	}

	// Verify session parts in DB
	checkSession := &core.StorageUploadSession{}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[bucket_id]], [[name]], [[parts]], [[total_size]], [[status]]
		FROM {{_storage_uploads}}
		WHERE [[id]] = {:id}
	`).Bind(map[string]any{"id": session.Id}).One(checkSession)
	if err != nil {
		t.Fatalf("failed to query session after concurrent uploads: %v", err)
	}

	if len(checkSession.Parts) != numParts {
		t.Fatalf("expected %d parts in session, got %d (lost parts detected!)", numParts, len(checkSession.Parts))
	}

	expectedTotalSize := int64(numParts * partSize)
	if checkSession.TotalSize != expectedTotalSize {
		t.Fatalf("expected total size %d, got %d", expectedTotalSize, checkSession.TotalSize)
	}

	// Verify parts are strictly ordered 1..10
	for i, part := range checkSession.Parts {
		if part.PartNumber != i+1 {
			t.Fatalf("expected part %d at index %d, got part %d", i+1, i, part.PartNumber)
		}
	}

	// Complete multipart upload
	ctx := context.Background()
	obj, err := sm.CompleteMultipartUpload(ctx, session.Id)
	if err != nil {
		t.Fatalf("CompleteMultipartUpload failed: %v", err)
	}

	if obj.Size != expectedTotalSize {
		t.Fatalf("expected completed object size %d, got %d", expectedTotalSize, obj.Size)
	}

	expectedHash := sha256.Sum256(expectedAssembled.Bytes())
	expectedEtag := hex.EncodeToString(expectedHash[:])
	if obj.Etag != expectedEtag {
		t.Fatalf("expected object ETag %s, got %s", expectedEtag, obj.Etag)
	}

	// Verify retrieved object matches assembled bytes exactly
	_, retrievedData, err := sm.GetObject(ctx, bucketName, "concurrent_object.bin", "tenant_test", false)
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}

	if !bytes.Equal(retrievedData, expectedAssembled.Bytes()) {
		t.Fatalf("retrieved object content does not match expected assembled bytes")
	}

	// Verify second CompleteMultipartUpload fails safely (cannot complete twice)
	_, err = sm.CompleteMultipartUpload(ctx, session.Id)
	if err == nil {
		t.Fatalf("expected error on duplicate CompleteMultipartUpload, got nil")
	}
}

func TestStorageManager_MultipartUploadValidationAndAbort(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-validation"
	_, err = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}

	// 1. Incomplete parts (missing part 2: uploaded 1 and 3)
	session1, _ := sm.StartMultipartUpload(bucketName, "missing_parts.bin", "tenant_test")
	_, err = sm.UploadPart(session1.Id, 1, []byte("part-1-data"))
	if err != nil {
		t.Fatalf("upload part 1 failed: %v", err)
	}
	_, err = sm.UploadPart(session1.Id, 3, []byte("part-3-data"))
	if err != nil {
		t.Fatalf("upload part 3 failed: %v", err)
	}

	ctx := context.Background()
	_, err = sm.CompleteMultipartUpload(ctx, session1.Id)
	if err == nil {
		t.Fatalf("expected failure when completing with missing part 2, got success")
	}

	// 2. Abort multipart upload
	session2, _ := sm.StartMultipartUpload(bucketName, "abort_test.bin", "tenant_test")
	_, _ = sm.UploadPart(session2.Id, 1, []byte("part-data-to-abort"))

	err = sm.AbortMultipartUpload(session2.Id)
	if err != nil {
		t.Fatalf("AbortMultipartUpload failed: %v", err)
	}

	// Complete on aborted upload must fail
	_, err = sm.CompleteMultipartUpload(ctx, session2.Id)
	if err == nil {
		t.Fatalf("expected failure on completed aborted upload, got nil")
	}
}
