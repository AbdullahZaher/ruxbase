package core_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// 1. 100 concurrent uploads to distinct parts.
func TestStorageManager_Multipart_100ConcurrentDistinctParts(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-100parts"
	_, err = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 50 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("failed to create bucket: %v", err)
	}

	session, err := sm.StartMultipartUpload(bucketName, "100parts.bin", "tenant_100")
	if err != nil {
		t.Fatalf("StartMultipartUpload failed: %v", err)
	}

	const totalParts = 100
	partDataMap := make(map[int][]byte, totalParts)
	var expectedAssembled bytes.Buffer

	for p := 1; p <= totalParts; p++ {
		data := bytes.Repeat([]byte(fmt.Sprintf("[%03d]", p)), 256) // 1280 bytes per part
		partDataMap[p] = data
		expectedAssembled.Write(data)
	}

	var wg sync.WaitGroup
	errs := make(chan error, totalParts)

	for p := 1; p <= totalParts; p++ {
		wg.Add(1)
		go func(partNum int, data []byte) {
			defer wg.Done()
			_, uploadErr := sm.UploadPart(session.Id, partNum, data)
			if uploadErr != nil {
				errs <- fmt.Errorf("part %d failed: %w", partNum, uploadErr)
			}
		}(p, partDataMap[p])
	}
	wg.Wait()
	close(errs)

	for e := range errs {
		t.Fatalf("concurrent upload error: %v", e)
	}

	ctx := context.Background()
	obj, err := sm.CompleteMultipartUpload(ctx, session.Id)
	if err != nil {
		t.Fatalf("CompleteMultipartUpload failed: %v", err)
	}

	if obj.Size != int64(expectedAssembled.Len()) {
		t.Fatalf("expected assembled size %d, got %d", expectedAssembled.Len(), obj.Size)
	}

	_, retrievedData, err := sm.GetObject(ctx, bucketName, "100parts.bin", "tenant_100", false)
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}

	if !bytes.Equal(retrievedData, expectedAssembled.Bytes()) {
		t.Fatalf("retrieved bytes do not match expected 100-part assembled bytes")
	}
}

// 2. Concurrent uploads to the same part number with different bytes.
func TestStorageManager_Multipart_ConcurrentSamePartDifferentBytes(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-samepart"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	session, err := sm.StartMultipartUpload(bucketName, "samepart.bin", "tenant_test")
	if err != nil {
		t.Fatalf("StartMultipartUpload failed: %v", err)
	}

	// Two concurrent uploads to Part 1 with completely different bytes and sizes
	bytesA := bytes.Repeat([]byte("A"), 1024)
	bytesB := bytes.Repeat([]byte("B"), 2048)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = sm.UploadPart(session.Id, 1, bytesA)
	}()
	go func() {
		defer wg.Done()
		_, _ = sm.UploadPart(session.Id, 1, bytesB)
	}()
	wg.Wait()

	// Complete upload
	ctx := context.Background()
	obj, err := sm.CompleteMultipartUpload(ctx, session.Id)
	if err != nil {
		t.Fatalf("CompleteMultipartUpload failed: %v", err)
	}

	// Fetch object: it MUST match either bytesA or bytesB exactly, NEVER a corrupted combination
	_, retrieved, err := sm.GetObject(ctx, bucketName, "samepart.bin", "tenant_test", false)
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}

	matchesA := bytes.Equal(retrieved, bytesA)
	matchesB := bytes.Equal(retrieved, bytesB)

	if !matchesA && !matchesB {
		t.Fatalf("data corruption: assembled object matched neither candidate! got %d bytes", len(retrieved))
	}
	if matchesA && obj.Size != int64(len(bytesA)) {
		t.Fatalf("size mismatch for candidate A")
	}
	if matchesB && obj.Size != int64(len(bytesB)) {
		t.Fatalf("size mismatch for candidate B")
	}
}

// 3. Concurrent retry of an identical part.
func TestStorageManager_Multipart_ConcurrentRetryIdenticalPart(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-retry"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	session, _ := sm.StartMultipartUpload(bucketName, "retry.bin", "tenant_test")
	data := []byte("consistent-part-data-1234567890")

	var wg sync.WaitGroup
	var errCount atomic.Int64
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := sm.UploadPart(session.Id, 1, data)
			if err != nil {
				errCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if errCount.Load() != 0 {
		t.Fatalf("expected 0 errors on identical part retries, got %d", errCount.Load())
	}

	ctx := context.Background()
	obj, err := sm.CompleteMultipartUpload(ctx, session.Id)
	if err != nil {
		t.Fatalf("CompleteMultipartUpload failed: %v", err)
	}
	if obj.Size != int64(len(data)) {
		t.Fatalf("expected size %d, got %d", len(data), obj.Size)
	}
}

// 4. Upload versus abort.
func TestStorageManager_Multipart_UploadVsAbort(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-up-abort"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	session, _ := sm.StartMultipartUpload(bucketName, "up_abort.bin", "tenant_test")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for p := 1; p <= 10; p++ {
			_, _ = sm.UploadPart(session.Id, p, []byte("data"))
			time.Sleep(2 * time.Millisecond)
		}
	}()
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		_ = sm.AbortMultipartUpload(session.Id)
	}()
	wg.Wait()

	// Subsequent complete MUST fail
	ctx := context.Background()
	_, err = sm.CompleteMultipartUpload(ctx, session.Id)
	if err == nil {
		t.Fatalf("expected complete to fail on aborted session")
	}
}

// 5. Complete versus abort (abort cannot delete parts being assembled by completing operation).
func TestStorageManager_Multipart_CompleteVsAbort(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-comp-abort"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	session, _ := sm.StartMultipartUpload(bucketName, "comp_abort.bin", "tenant_test")
	_, _ = sm.UploadPart(session.Id, 1, []byte("first-part-bytes"))
	_, _ = sm.UploadPart(session.Id, 2, []byte("second-part-bytes"))

	// Manually set status to 'completing' to simulate in-flight assembly
	_, err = app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[status]] = 'completing' WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": session.Id}).Execute()
	if err != nil {
		t.Fatalf("failed to set status completing: %v", err)
	}

	// Abort MUST be rejected while completing
	err = sm.AbortMultipartUpload(session.Id)
	if err == nil {
		t.Fatalf("expected AbortMultipartUpload to fail while status is completing")
	}
}

// 6. Concurrent duplicate completion attempts.
func TestStorageManager_Multipart_ConcurrentDuplicateCompletion(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-dup-comp"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	session, _ := sm.StartMultipartUpload(bucketName, "dup_comp.bin", "tenant_test")
	_, _ = sm.UploadPart(session.Id, 1, []byte("part-1-test-data"))
	_, _ = sm.UploadPart(session.Id, 2, []byte("part-2-test-data"))

	ctx := context.Background()
	var successCount atomic.Int64
	var failCount atomic.Int64
	var wg sync.WaitGroup

	const concurrency = 10
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := sm.CompleteMultipartUpload(ctx, session.Id)
			if err == nil {
				successCount.Add(1)
			} else {
				failCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if successCount.Load() != 1 {
		t.Fatalf("expected exactly 1 successful completion, got %d", successCount.Load())
	}
	if failCount.Load() != concurrency-1 {
		t.Fatalf("expected %d failed duplicate completions, got %d", concurrency-1, failCount.Load())
	}
}

// 7. Checksum mismatch detection.
func TestStorageManager_Multipart_ChecksumMismatch(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-checksum"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	session, _ := sm.StartMultipartUpload(bucketName, "corrupt.bin", "tenant_test")
	part1, _ := sm.UploadPart(session.Id, 1, []byte("original-unmodified-bytes"))

	// Tamper with the file on the filesystem directly behind the database's back
	fsys, err := app.NewFilesystem()
	if err != nil {
		t.Fatalf("failed to get filesystem: %v", err)
	}
	defer fsys.Close()

	tamperedData := []byte("tampered-corrupted-bytes!!")
	if err := fsys.Upload(tamperedData, part1.StorageKey); err != nil {
		t.Fatalf("failed to tamper with file: %v", err)
	}

	ctx := context.Background()
	_, err = sm.CompleteMultipartUpload(ctx, session.Id)
	if err == nil {
		t.Fatalf("expected CompleteMultipartUpload to detect checksum mismatch, but it succeeded")
	}

	// Verify session was safely reverted to 'pending'
	var status string
	_ = app.DB().NewQuery("SELECT [[status]] FROM {{_storage_uploads}} WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": session.Id}).Row(&status)
	if status != "pending" {
		t.Fatalf("expected session status to be reverted to pending, got %s", status)
	}
}

// 8. Crash recovery after object upload before completion acknowledgment.
func TestStorageManager_Multipart_CrashAfterObjectUploadRecovery(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-crash-rec"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	session, _ := sm.StartMultipartUpload(bucketName, "crash_rec.bin", "tenant_test")
	partData := []byte("resilient-data-for-crash-recovery")
	_, _ = sm.UploadPart(session.Id, 1, partData)

	ctx := context.Background()

	// Simulate crash: Object was created in storage & _storage_objects, but session remains in 'completing'
	obj, err := sm.UploadObject(ctx, bucketName, "crash_rec.bin", "tenant_test", "application/octet-stream", partData, nil)
	if err != nil {
		t.Fatalf("UploadObject failed: %v", err)
	}

	_, err = app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[status]] = 'completing' WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": session.Id}).Execute()
	if err != nil {
		t.Fatalf("failed to set status completing: %v", err)
	}

	// CompleteMultipartUpload called again (e.g. on worker restart)
	recoveredObj, err := sm.CompleteMultipartUpload(ctx, session.Id)
	if err != nil {
		t.Fatalf("CompleteMultipartUpload failed to recover crashed session: %v", err)
	}

	if recoveredObj.Id != obj.Id || recoveredObj.Size != obj.Size {
		t.Fatalf("recovered object does not match original: want ID %s, got %s", obj.Id, recoveredObj.Id)
	}

	// Verify session status is now 'completed'
	var status string
	_ = app.DB().NewQuery("SELECT [[status]] FROM {{_storage_uploads}} WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": session.Id}).Row(&status)
	if status != "completed" {
		t.Fatalf("expected completed status after recovery, got %s", status)
	}
}

// 9. Cleanup of orphaned temporary files without deleting active pending sessions.
func TestStorageManager_Multipart_CleanupOrphanedUploads(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-gc"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	// Active pending session
	activeSession, _ := sm.StartMultipartUpload(bucketName, "active.bin", "tenant_test")
	activePart, _ := sm.UploadPart(activeSession.Id, 1, []byte("active-session-bytes"))

	// Abandoned session: created with updated backdated to 2 days ago
	oldSession, _ := sm.StartMultipartUpload(bucketName, "old.bin", "tenant_test")
	oldPart, _ := sm.UploadPart(oldSession.Id, 1, []byte("old-session-bytes"))
	_, _ = app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[updated]] = now() - INTERVAL '48 hours' WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": oldSession.Id}).Execute()

	ctx := context.Background()
	purged, err := sm.CleanupOrphanedUploads(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("CleanupOrphanedUploads error: %v", err)
	}

	if purged == 0 {
		t.Fatalf("expected at least 1 purged orphaned chunk, got 0")
	}

	// Verify active session part still exists!
	fsys, err := app.NewFilesystem()
	if err != nil {
		t.Fatalf("failed to get filesystem: %v", err)
	}
	defer fsys.Close()

	if exists, _ := fsys.Exists(activePart.StorageKey); !exists {
		t.Fatalf("active session part was incorrectly deleted by GC!")
	}

	// Verify old session part was deleted
	if exists, _ := fsys.Exists(oldPart.StorageKey); exists {
		t.Fatalf("old session part was not deleted by GC!")
	}
}
