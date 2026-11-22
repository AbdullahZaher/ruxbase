package core_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
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

// 8. Crash recovery after session-bound metadata commit before status acknowledgment.
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

	// Inject deterministic crash right AFTER _storage_objects commit and BEFORE status='completed' ack
	var committedObj *core.StorageObject
	core.SetStorageFaultHooksForTest(sm, &core.StorageFaultHooksForTest{
		AfterMetadataCommit: func(uploadId string, obj *core.StorageObject) error {
			committedObj = obj
			return errors.New("simulated crash before completion status ack")
		},
	})

	// Use short 100ms lease so recovery can proceed deterministically after real lease expiry
	_, err = sm.CompleteMultipartUploadWithLease(ctx, session.Id, 100*time.Millisecond)
	if err == nil {
		t.Fatalf("expected simulated crash error, got nil")
	}
	if committedObj == nil {
		t.Fatalf("expected committed object before simulated crash")
	}
	if committedObj.UploadId != session.Id {
		t.Fatalf("expected committed object upload_id=%q, got %q", session.Id, committedObj.UploadId)
	}

	// Clear fault hook and wait for the 100ms completion lease to expire naturally
	core.SetStorageFaultHooksForTest(sm, nil)
	time.Sleep(150 * time.Millisecond)

	// Recovery on retry / worker restart verifies session binding and stored blob SHA-256
	recoveredObj, err := sm.CompleteMultipartUpload(ctx, session.Id)
	if err != nil {
		t.Fatalf("CompleteMultipartUpload failed to recover crashed session: %v", err)
	}

	if recoveredObj.Id != committedObj.Id || recoveredObj.Size != committedObj.Size || recoveredObj.Etag != committedObj.Etag {
		t.Fatalf("recovered object does not match committed object: want %+v, got %+v", committedObj, recoveredObj)
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

	// Verify old session is now marked 'expired' with ZERO dangling part references
	var oldSess core.StorageUploadSession
	if err := app.DB().NewQuery("SELECT [[id]], [[status]], [[parts]], [[total_size]] FROM {{_storage_uploads}} WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": oldSession.Id}).One(&oldSess); err != nil {
		t.Fatalf("failed to load expired session: %v", err)
	}
	if oldSess.Status != "expired" {
		t.Fatalf("expected old session status 'expired', got %q", oldSess.Status)
	}
	if len(oldSess.Parts) != 0 || oldSess.TotalSize != 0 {
		t.Fatalf("expected expired session to have 0 dangling parts, got %d parts (totalSize=%d)", len(oldSess.Parts), oldSess.TotalSize)
	}

	// Expired session MUST reject resume attempts
	if _, err := sm.UploadPart(oldSession.Id, 2, []byte("late-chunk")); !errors.Is(err, core.ErrStorageUploadExpired) {
		t.Fatalf("expected ErrStorageUploadExpired on UploadPart for expired session, got %v", err)
	}
	if _, err := sm.CompleteMultipartUpload(ctx, oldSession.Id); !errors.Is(err, core.ErrStorageUploadExpired) {
		t.Fatalf("expected ErrStorageUploadExpired on CompleteMultipartUpload for expired session, got %v", err)
	}
	if err := sm.AbortMultipartUpload(oldSession.Id); !errors.Is(err, core.ErrStorageUploadExpired) {
		t.Fatalf("expected ErrStorageUploadExpired on AbortMultipartUpload for expired session, got %v", err)
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

// 10. Existing unrelated object with SAME bucket, name, and size but DIFFERENT SHA-256 MUST be rejected.
func TestStorageManager_Multipart_RejectUnrelatedObjectSameNameSizeDifferentSHA(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-unrelated"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	ctx := context.Background()
	preexistingBytes := []byte("PREEXISTING_BYTES_EXACT_LEN_32!!")
	newSessionBytes := []byte("NEW_SESSION_BYTES_EXACT_LEN_32!!")
	if len(preexistingBytes) != len(newSessionBytes) {
		t.Fatalf("test setup error: lengths must match")
	}

	preObj, err := sm.UploadObject(ctx, bucketName, "shared_name.bin", "tenant_test", "application/octet-stream", preexistingBytes, nil)
	if err != nil {
		t.Fatalf("preexisting UploadObject failed: %v", err)
	}

	session, err := sm.StartMultipartUpload(bucketName, "shared_name.bin", "tenant_test")
	if err != nil {
		t.Fatalf("StartMultipartUpload failed: %v", err)
	}
	_, err = sm.UploadPart(session.Id, 1, newSessionBytes)
	if err != nil {
		t.Fatalf("UploadPart failed: %v", err)
	}

	// Case A: Fresh completion from pending must NOT overwrite unrelated preexisting object
	if _, err := sm.CompleteMultipartUpload(ctx, session.Id); !errors.Is(err, core.ErrStorageObjectConflict) {
		t.Fatalf("expected ErrStorageObjectConflict when completing over unrelated preexisting object, got %v", err)
	}

	// Case B: Crash recovery from 'completing' (expired lease) MUST NOT adopt unrelated preexisting object
	_, err = app.DB().NewQuery(`
		UPDATE {{_storage_uploads}}
		SET [[status]] = 'completing',
		    [[completion_leased_until]] = now() - INTERVAL '10 seconds'
		WHERE [[id]] = {:id}
	`).Bind(map[string]any{"id": session.Id}).Execute()
	if err != nil {
		t.Fatalf("failed to set completing state: %v", err)
	}

	if _, err := sm.CompleteMultipartUpload(ctx, session.Id); !errors.Is(err, core.ErrStorageObjectConflict) {
		t.Fatalf("expected ErrStorageObjectConflict during recovery against unrelated preexisting object, got %v", err)
	}

	// Verify preexisting object bytes and metadata remain 100% untouched
	gotObj, gotBytes, err := sm.GetObject(ctx, bucketName, "shared_name.bin", "tenant_test", false)
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	if gotObj.Id != preObj.Id || !bytes.Equal(gotBytes, preexistingBytes) {
		t.Fatalf("preexisting object was mutated or overwritten!")
	}
}

// 11. Existing object from ANOTHER multipart session (even with identical bytes) MUST NOT be adopted.
func TestStorageManager_Multipart_RejectObjectFromAnotherSession(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-cross-sess"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	ctx := context.Background()
	identicalBytes := []byte("identical-payload-from-session-one")

	session1, _ := sm.StartMultipartUpload(bucketName, "target.bin", "tenant_test")
	_, _ = sm.UploadPart(session1.Id, 1, identicalBytes)
	obj1, err := sm.CompleteMultipartUpload(ctx, session1.Id)
	if err != nil {
		t.Fatalf("session1 CompleteMultipartUpload failed: %v", err)
	}
	if obj1.UploadId != session1.Id {
		t.Fatalf("expected obj1.UploadId=%q, got %q", session1.Id, obj1.UploadId)
	}

	// Start session2 targeting the same object with identical bytes and simulate crashed completing state
	session2, _ := sm.StartMultipartUpload(bucketName, "target.bin", "tenant_test")
	_, _ = sm.UploadPart(session2.Id, 1, identicalBytes)
	_, _ = app.DB().NewQuery(`
		UPDATE {{_storage_uploads}}
		SET [[status]] = 'completing',
		    [[completion_leased_until]] = now() - INTERVAL '10 seconds'
		WHERE [[id]] = {:id}
	`).Bind(map[string]any{"id": session2.Id}).Execute()

	if _, err := sm.CompleteMultipartUpload(ctx, session2.Id); !errors.Is(err, core.ErrStorageObjectConflict) {
		t.Fatalf("expected ErrStorageObjectConflict when session2 attempts to adopt session1 object, got %v", err)
	}

	// Confirm session2 was NOT marked completed
	var status2 string
	_ = app.DB().NewQuery("SELECT [[status]] FROM {{_storage_uploads}} WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": session2.Id}).Row(&status2)
	if status2 == "completed" {
		t.Fatalf("session2 was falsely marked completed using session1's object!")
	}
}

// 12. Crash after final blob write but BEFORE _storage_objects metadata commit is safely recoverable.
func TestStorageManager_Multipart_CrashAfterBlobWriteBeforeMetadataCommit(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-crash-blob"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	session, _ := sm.StartMultipartUpload(bucketName, "blob_crash.bin", "tenant_test")
	p1 := []byte("chunk-one-")
	p2 := []byte("chunk-two")
	_, _ = sm.UploadPart(session.Id, 1, p1)
	_, _ = sm.UploadPart(session.Id, 2, p2)

	ctx := context.Background()
	var writtenKey string
	core.SetStorageFaultHooksForTest(sm, &core.StorageFaultHooksForTest{
		AfterFinalBlobWrite: func(uploadId string, fileKey string) error {
			writtenKey = fileKey
			return errors.New("simulated crash after blob write before metadata commit")
		},
	})

	if _, err := sm.CompleteMultipartUploadWithLease(ctx, session.Id, 100*time.Millisecond); err == nil {
		t.Fatalf("expected error from simulated crash after blob write")
	}
	if writtenKey == "" {
		t.Fatalf("expected blob to have been written before crash")
	}

	// Verify no _storage_objects metadata row exists yet
	var objCount int
	_ = app.DB().NewQuery("SELECT count(*) FROM {{_storage_objects}} WHERE [[name]] = 'blob_crash.bin'").Row(&objCount)
	if objCount != 0 {
		t.Fatalf("expected 0 _storage_objects rows before metadata commit, got %d", objCount)
	}

	// Clear fault hook, wait for lease expiration, and recover cleanly
	core.SetStorageFaultHooksForTest(sm, nil)
	time.Sleep(150 * time.Millisecond)

	obj, err := sm.CompleteMultipartUpload(ctx, session.Id)
	if err != nil {
		t.Fatalf("recovery after blob-write crash failed: %v", err)
	}
	if obj.UploadId != session.Id {
		t.Fatalf("expected obj.UploadId=%q, got %q", session.Id, obj.UploadId)
	}

	_, gotData, err := sm.GetObject(ctx, bucketName, "blob_crash.bin", "tenant_test", false)
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	if string(gotData) != "chunk-one-chunk-two" {
		t.Fatalf("unexpected recovered content: %q", string(gotData))
	}
}

// 13. A live in-flight completion MUST NOT be stolen or classified as abandoned by a concurrent request.
func TestStorageManager_Multipart_LiveCompletionNotStolenOrAbandoned(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-live-owner"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	session, _ := sm.StartMultipartUpload(bucketName, "live.bin", "tenant_test")
	_, _ = sm.UploadPart(session.Id, 1, []byte("live-completion-bytes"))

	claimedCh := make(chan struct{})
	releaseCh := make(chan struct{})
	var hookOnce sync.Once
	core.SetStorageFaultHooksForTest(sm, &core.StorageFaultHooksForTest{
		AfterCompletionClaim: func(uploadId string, completionToken string) error {
			hookOnce.Do(func() {
				close(claimedCh)
				<-releaseCh
			})
			return nil
		},
	})
	defer core.SetStorageFaultHooksForTest(sm, nil)

	ctx := context.Background()
	worker1Err := make(chan error, 1)
	go func() {
		_, err := sm.CompleteMultipartUploadWithLease(ctx, session.Id, 30*time.Second)
		worker1Err <- err
	}()

	<-claimedCh

	// Concurrent Worker 2 attempts completion while Worker 1 holds active 30s lease
	_, err2 := sm.CompleteMultipartUploadWithLease(ctx, session.Id, 30*time.Second)
	if !errors.Is(err2, core.ErrStorageUploadInProgress) {
		t.Fatalf("expected ErrStorageUploadInProgress for concurrent worker during live completion, got %v", err2)
	}

	// Concurrent Abort MUST also be rejected
	if abortErr := sm.AbortMultipartUpload(session.Id); abortErr == nil {
		t.Fatalf("expected AbortMultipartUpload to fail during live completion")
	}

	close(releaseCh)
	if err := <-worker1Err; err != nil {
		t.Fatalf("Worker 1 live completion failed: %v", err)
	}
}

// 14. Recovery with missing or corrupted final-object bytes MUST fail closed.
func TestStorageManager_Multipart_RecoveryFailsClosedOnMissingOrCorruptedBlob(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-corrupt-rec"
	b, _ := sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	ctx := context.Background()

	// Subcase A: Corrupted final blob bytes
	sessCorrupt, _ := sm.StartMultipartUpload(bucketName, "corrupted_final.bin", "tenant_test")
	_, _ = sm.UploadPart(sessCorrupt.Id, 1, []byte("expected-valid-bytes"))

	core.SetStorageFaultHooksForTest(sm, &core.StorageFaultHooksForTest{
		AfterMetadataCommit: func(uploadId string, obj *core.StorageObject) error {
			return errors.New("crash before status ack")
		},
	})
	_, _ = sm.CompleteMultipartUploadWithLease(ctx, sessCorrupt.Id, 100*time.Millisecond)
	core.SetStorageFaultHooksForTest(sm, nil)
	time.Sleep(150 * time.Millisecond)

	fsys, _ := app.NewFilesystem()
	defer fsys.Close()
	if err := fsys.Upload([]byte("CORRUPTED-SAME-LEN!!"), path.Join("_storage", b.Id, "corrupted_final.bin")); err != nil {
		t.Fatalf("failed to tamper final blob: %v", err)
	}

	if _, err := sm.CompleteMultipartUpload(ctx, sessCorrupt.Id); !errors.Is(err, core.ErrStorageIntegrityMismatch) {
		t.Fatalf("expected ErrStorageIntegrityMismatch on corrupted final blob, got %v", err)
	}
	var stCorrupt string
	_ = app.DB().NewQuery("SELECT [[status]] FROM {{_storage_uploads}} WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": sessCorrupt.Id}).Row(&stCorrupt)
	if stCorrupt == "completed" {
		t.Fatalf("corrupted recovery falsely marked session completed!")
	}

	// Subcase B: Missing final blob bytes
	sessMissing, _ := sm.StartMultipartUpload(bucketName, "missing_final.bin", "tenant_test")
	_, _ = sm.UploadPart(sessMissing.Id, 1, []byte("vanished-final-bytes"))

	core.SetStorageFaultHooksForTest(sm, &core.StorageFaultHooksForTest{
		AfterMetadataCommit: func(uploadId string, obj *core.StorageObject) error {
			return errors.New("crash before status ack")
		},
	})
	_, _ = sm.CompleteMultipartUploadWithLease(ctx, sessMissing.Id, 100*time.Millisecond)
	core.SetStorageFaultHooksForTest(sm, nil)
	time.Sleep(150 * time.Millisecond)

	if err := fsys.Delete(path.Join("_storage", b.Id, "missing_final.bin")); err != nil {
		t.Fatalf("failed to delete final blob: %v", err)
	}

	if _, err := sm.CompleteMultipartUpload(ctx, sessMissing.Id); !errors.Is(err, core.ErrStorageIntegrityMismatch) {
		t.Fatalf("expected ErrStorageIntegrityMismatch on missing final blob, got %v", err)
	}
}

// 15. Recovery remains safe across application restart and idempotent across retries.
func TestStorageManager_Multipart_RecoveryAfterAppRestartAndRetry(t *testing.T) {
	app1, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app 1: %v", err)
	}
	defer app1.Cleanup()

	sm1 := app1.Storage()
	bucketName := "test-multipart-restart"
	_, _ = sm1.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	session, _ := sm1.StartMultipartUpload(bucketName, "restart_rec.bin", "tenant_test")
	payload := []byte("persisted-across-app-restart")
	_, _ = sm1.UploadPart(session.Id, 1, payload)

	ctx := context.Background()
	core.SetStorageFaultHooksForTest(sm1, &core.StorageFaultHooksForTest{
		AfterMetadataCommit: func(uploadId string, obj *core.StorageObject) error {
			return errors.New("simulated process crash before status ack")
		},
	})
	_, _ = sm1.CompleteMultipartUploadWithLease(ctx, session.Id, 100*time.Millisecond)
	core.SetStorageFaultHooksForTest(sm1, nil)
	time.Sleep(150 * time.Millisecond)

	// Simulate fresh process / application restart pointing at the same database and data directory
	app2 := core.NewBaseApp(core.BaseAppConfig{
		DataDir:          app1.DataDir(),
		DatabaseURL:      app1.DatabaseURL(),
		DataMaxOpenConns: 4,
		AuxMaxOpenConns:  2,
	})
	if err := app2.Bootstrap(); err != nil {
		t.Fatalf("failed to bootstrap restarted app2: %v", err)
	}
	defer app2.ClearBootstrap()

	obj, err := app2.Storage().CompleteMultipartUpload(ctx, session.Id)
	if err != nil {
		t.Fatalf("restarted app2 recovery failed: %v", err)
	}
	if obj.UploadId != session.Id {
		t.Fatalf("expected recovered object upload_id=%q, got %q", session.Id, obj.UploadId)
	}

	// Repeat call after successful completion fails closed with ErrStorageUploadAlreadyComplete
	if _, err := app2.Storage().CompleteMultipartUpload(ctx, session.Id); !errors.Is(err, core.ErrStorageUploadAlreadyComplete) {
		t.Fatalf("expected ErrStorageUploadAlreadyComplete on post-recovery repeat, got %v", err)
	}
}

// 16. Two distinct sessions targeting the SAME (bucket, name) concurrently are serialized by partial unique index.
func TestStorageManager_Multipart_CompetingSessionsSameTarget(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-competing"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	s1, _ := sm.StartMultipartUpload(bucketName, "same_target.bin", "tenant_test")
	s2, _ := sm.StartMultipartUpload(bucketName, "same_target.bin", "tenant_test")
	bytes1 := bytes.Repeat([]byte("1"), 512)
	bytes2 := bytes.Repeat([]byte("2"), 512)
	_, _ = sm.UploadPart(s1.Id, 1, bytes1)
	_, _ = sm.UploadPart(s2.Id, 1, bytes2)

	ctx := context.Background()
	var okCount, conflictCount atomic.Int64
	var wg sync.WaitGroup
	startBarrier := make(chan struct{})

	for _, id := range []string{s1.Id, s2.Id} {
		wg.Add(1)
		go func(uploadId string) {
			defer wg.Done()
			<-startBarrier
			_, err := sm.CompleteMultipartUpload(ctx, uploadId)
			if err == nil {
				okCount.Add(1)
			} else if errors.Is(err, core.ErrStorageObjectConflict) {
				conflictCount.Add(1)
			} else {
				t.Errorf("unexpected error for %s: %v", uploadId, err)
			}
		}(id)
	}
	close(startBarrier)
	wg.Wait()

	if okCount.Load() != 1 || conflictCount.Load() != 1 {
		t.Fatalf("expected 1 success and 1 conflict, got ok=%d conflict=%d", okCount.Load(), conflictCount.Load())
	}

	obj, gotBytes, err := sm.GetObject(ctx, bucketName, "same_target.bin", "tenant_test", false)
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	if obj.UploadId == s1.Id && !bytes.Equal(gotBytes, bytes1) {
		t.Fatalf("stored bytes do not match winning session s1")
	}
	if obj.UploadId == s2.Id && !bytes.Equal(gotBytes, bytes2) {
		t.Fatalf("stored bytes do not match winning session s2")
	}
}

// 17. Concurrent UploadPart vs GC: no active chunk loss, and expired session rejects late upload cleanly.
func TestStorageManager_Multipart_GC_ConcurrentUploadPartVsGC(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-gc-up"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	// Stale session that will race UploadPart against GC
	staleSession, _ := sm.StartMultipartUpload(bucketName, "race_gc.bin", "tenant_test")
	p1, _ := sm.UploadPart(staleSession.Id, 1, []byte("part-1"))
	_, _ = app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[updated]] = now() - INTERVAL '48 hours' WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": staleSession.Id}).Execute()

	// Pause UploadPart(part 2) right AFTER blob write and BEFORE DB transaction so GC Stage A expires the session first
	partUploadedCh := make(chan string, 1)
	releaseUploadCh := make(chan struct{})
	core.SetStorageFaultHooksForTest(sm, &core.StorageFaultHooksForTest{
		BeforePartCommit: func(uploadId string, partNumber int, partKey string) error {
			if uploadId == staleSession.Id && partNumber == 2 {
				partUploadedCh <- partKey
				<-releaseUploadCh
			}
			return nil
		},
	})
	defer core.SetStorageFaultHooksForTest(sm, nil)

	uploadErrCh := make(chan error, 1)
	go func() {
		_, err := sm.UploadPart(staleSession.Id, 2, []byte("late-part-2"))
		uploadErrCh <- err
	}()

	part2Key := <-partUploadedCh

	// Run GC now: Stage A expires staleSession, Stage B cleans up p1 (and leaves nothing dangling)
	ctx := context.Background()
	if _, err := sm.CleanupOrphanedUploads(ctx, 24*time.Hour); err != nil {
		t.Fatalf("CleanupOrphanedUploads failed: %v", err)
	}

	// Release paused UploadPart: it must detect status='expired', reject with ErrStorageUploadExpired, and remove part2Key
	close(releaseUploadCh)
	if err := <-uploadErrCh; !errors.Is(err, core.ErrStorageUploadExpired) {
		t.Fatalf("expected ErrStorageUploadExpired for racing UploadPart after GC Stage A, got %v", err)
	}

	fsys, _ := app.NewFilesystem()
	defer fsys.Close()
	if exists, _ := fsys.Exists(p1.StorageKey); exists {
		t.Fatalf("expected expired part 1 to be deleted")
	}
	if exists, _ := fsys.Exists(part2Key); exists {
		t.Fatalf("expected rejected racing part 2 to be cleaned up")
	}
}

// 18. Concurrent CompleteMultipartUpload vs GC: GC strictly preserves completing sessions even when aged.
func TestStorageManager_Multipart_GC_ConcurrentCompleteVsGC(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-gc-comp"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})

	session, _ := sm.StartMultipartUpload(bucketName, "comp_vs_gc.bin", "tenant_test")
	p1, _ := sm.UploadPart(session.Id, 1, []byte("important-part-bytes"))

	claimedCh := make(chan struct{})
	releaseCh := make(chan struct{})
	core.SetStorageFaultHooksForTest(sm, &core.StorageFaultHooksForTest{
		AfterCompletionClaim: func(uploadId string, completionToken string) error {
			close(claimedCh)
			<-releaseCh
			return nil
		},
	})
	defer core.SetStorageFaultHooksForTest(sm, nil)

	ctx := context.Background()
	compErrCh := make(chan error, 1)
	go func() {
		_, err := sm.CompleteMultipartUpload(ctx, session.Id)
		compErrCh <- err
	}()

	<-claimedCh

	// Even if updated timestamp is old, session is in 'completing' so GC MUST NOT expire or delete its parts
	_, _ = app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[updated]] = now() - INTERVAL '48 hours' WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": session.Id}).Execute()

	if _, err := sm.CleanupOrphanedUploads(ctx, 24*time.Hour); err != nil {
		t.Fatalf("CleanupOrphanedUploads failed: %v", err)
	}

	fsys, _ := app.NewFilesystem()
	defer fsys.Close()
	if exists, _ := fsys.Exists(p1.StorageKey); !exists {
		t.Fatalf("GC deleted part of an actively completing session!")
	}

	close(releaseCh)
	if err := <-compErrCh; err != nil {
		t.Fatalf("CompleteMultipartUpload failed after GC ran: %v", err)
	}
}

// 19. Concurrent AbortMultipartUpload vs GC, multiple GC workers, object-store delete failure, and crash recovery during GC.
func TestStorageManager_Multipart_GC_FailuresCrashAndMultiWorkerIdempotency(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	sm := app.Storage()
	bucketName := "test-multipart-gc-faults"
	_, _ = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})
	ctx := context.Background()

	// Case A: Object-store deletion failure MUST be returned as error and NOT counted as purged
	sFail, _ := sm.StartMultipartUpload(bucketName, "del_fail.bin", "tenant_test")
	pFail, _ := sm.UploadPart(sFail.Id, 1, []byte("delete-failure-chunk"))
	_, _ = app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[updated]] = now() - INTERVAL '48 hours' WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": sFail.Id}).Execute()

	core.SetStorageFaultHooksForTest(sm, &core.StorageFaultHooksForTest{
		BeforeGCDeleteKey: func(storageKey string) error {
			if storageKey == pFail.StorageKey {
				return errors.New("simulated S3/filesystem delete I/O failure")
			}
			return nil
		},
	})

	purged, gcErr := sm.CleanupOrphanedUploads(ctx, 24*time.Hour)
	if gcErr == nil {
		t.Fatalf("expected error from CleanupOrphanedUploads on delete failure, got nil")
	}
	if purged != 0 {
		t.Fatalf("expected purged=0 when delete failed, got %d", purged)
	}

	// Verify failure attempt was recorded in _storage_upload_cleanup journal
	var attempts int
	var lastErr string
	if err := app.DB().NewQuery("SELECT [[attempts]], [[last_error]] FROM {{_storage_upload_cleanup}} WHERE [[storage_key]] = {:k}").
		Bind(map[string]any{"k": pFail.StorageKey}).Row(&attempts, &lastErr); err != nil {
		t.Fatalf("expected cleanup journal entry to remain after delete failure: %v", err)
	}
	if attempts < 1 || lastErr == "" {
		t.Fatalf("expected recorded failure attempt and last_error, got attempts=%d lastErr=%q", attempts, lastErr)
	}

	// Clear fault and re-run: idempotent retry succeeds and purges the chunk
	core.SetStorageFaultHooksForTest(sm, nil)
	purged, gcErr = sm.CleanupOrphanedUploads(ctx, 24*time.Hour)
	if gcErr != nil || purged != 1 {
		t.Fatalf("expected retry GC to purge 1 chunk with nil err, got purged=%d err=%v", purged, gcErr)
	}

	// Case B: DB failure during Stage A fails cleanly
	core.SetStorageFaultHooksForTest(sm, &core.StorageFaultHooksForTest{
		BeforeGCStageA: func() error {
			return errors.New("simulated DB failure in Stage A")
		},
	})
	if _, err := sm.CleanupOrphanedUploads(ctx, 24*time.Hour); err == nil {
		t.Fatalf("expected Stage A DB failure to propagate")
	}

	// Case C: Crash between Stage A and Stage B -> session is already expired, repeated GC finishes Stage B idempotently
	sCrash, _ := sm.StartMultipartUpload(bucketName, "gc_crash.bin", "tenant_test")
	pCrash, _ := sm.UploadPart(sCrash.Id, 1, []byte("crash-between-stages"))
	_, _ = app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[updated]] = now() - INTERVAL '48 hours' WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": sCrash.Id}).Execute()

	core.SetStorageFaultHooksForTest(sm, &core.StorageFaultHooksForTest{
		AfterGCStageA: func(expiredUploadIds []string) error {
			return errors.New("simulated crash between Stage A and Stage B")
		},
	})
	if _, err := sm.CleanupOrphanedUploads(ctx, 24*time.Hour); err == nil {
		t.Fatalf("expected simulated crash error after Stage A")
	}

	// Session is already durably expired even before Stage B ran
	var stCrash string
	_ = app.DB().NewQuery("SELECT [[status]] FROM {{_storage_uploads}} WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": sCrash.Id}).Row(&stCrash)
	if stCrash != "expired" {
		t.Fatalf("expected status 'expired' after Stage A commit, got %q", stCrash)
	}

	// Case D: Legacy temporary keys + concurrent Abort vs GC + 4 concurrent GC workers
	fsys, _ := app.NewFilesystem()
	defer fsys.Close()

	// Create a legacy temporary key for an already-deleted/aborted session
	legacyOrphanKey := "_storage_uploads/legacy_deleted_sess/part_1"
	if err := fsys.Upload([]byte("legacy-orphan"), legacyOrphanKey); err != nil {
		t.Fatalf("failed to plant legacy orphan key: %v", err)
	}

	// Create an active session with a legacy key format to prove it is preserved
	activeLegacySess, _ := sm.StartMultipartUpload(bucketName, "active_legacy.bin", "tenant_test")
	activeLegacyKey := fmt.Sprintf("_storage_uploads/%s/part_1", activeLegacySess.Id)
	if err := fsys.Upload([]byte("active-legacy-data"), activeLegacyKey); err != nil {
		t.Fatalf("failed to plant active legacy key: %v", err)
	}
	legacyHash := sha256.Sum256([]byte("active-legacy-data"))
	legacyPartsJSON := fmt.Sprintf(`[{"partNumber":1,"size":%d,"etag":"%s"}]`, len("active-legacy-data"), hex.EncodeToString(legacyHash[:]))
	_, _ = app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[parts]] = {:p}::jsonb, [[total_size]] = {:s} WHERE [[id]] = {:id}").
		Bind(map[string]any{"p": legacyPartsJSON, "s": len("active-legacy-data"), "id": activeLegacySess.Id}).Execute()

	// Also race AbortMultipartUpload against 4 concurrent GC workers on another stale session
	sAbortRace, _ := sm.StartMultipartUpload(bucketName, "abort_gc_race.bin", "tenant_test")
	pAbortRace, _ := sm.UploadPart(sAbortRace.Id, 1, []byte("abort-vs-gc-bytes"))
	_, _ = app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[updated]] = now() - INTERVAL '48 hours' WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": sAbortRace.Id}).Execute()

	core.SetStorageFaultHooksForTest(sm, nil)

	var wg sync.WaitGroup
	var totalPurged atomic.Int64
	startBarrier := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-startBarrier
		_ = sm.AbortMultipartUpload(sAbortRace.Id)
	}()

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startBarrier
			n, err := sm.CleanupOrphanedUploads(ctx, 24*time.Hour)
			if err != nil {
				t.Errorf("concurrent GC worker error: %v", err)
			}
			totalPurged.Add(int64(n))
		}()
	}
	close(startBarrier)
	wg.Wait()

	// Run one final idempotent pass to ensure everything is clean
	extraPurged, err := sm.CleanupOrphanedUploads(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("final idempotent GC pass failed: %v", err)
	}
	if extraPurged != 0 {
		t.Fatalf("expected 0 purged on repeated idempotent GC pass, got %d", extraPurged)
	}

	// Verify all orphaned keys (pCrash, legacyOrphanKey, pAbortRace) are gone
	for _, goneKey := range []string{pCrash.StorageKey, legacyOrphanKey, pAbortRace.StorageKey} {
		if exists, _ := fsys.Exists(goneKey); exists {
			t.Fatalf("expected orphaned key %s to be deleted", goneKey)
		}
	}

	// Verify active legacy session key was preserved!
	if exists, _ := fsys.Exists(activeLegacyKey); !exists {
		t.Fatalf("active legacy key %s was incorrectly deleted by GC!", activeLegacyKey)
	}

	// Verify cleanup journal is completely drained
	var remainingTasks int
	_ = app.DB().NewQuery("SELECT count(*) FROM {{_storage_upload_cleanup}}").Row(&remainingTasks)
	if remainingTasks != 0 {
		t.Fatalf("expected 0 remaining cleanup journal tasks, got %d", remainingTasks)
	}
}

// 20. S3-compatible backend verification: multipart upload, crash recovery, and two-stage orphan GC via filesystem.NewS3.
func TestStorageManager_Multipart_S3BackendRecoveryAndGC(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// Minimal thread-safe in-memory S3-compatible HTTP server (path-style)
	var mu sync.RWMutex
	store := map[string][]byte{}
	modTimes := map[string]time.Time{}

	s3Srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path format: /test-bucket/<key> or /test-bucket?list-type=2&prefix=...
		trimmed := strings.TrimPrefix(r.URL.Path, "/")
		parts := strings.SplitN(trimmed, "/", 2)
		key := ""
		if len(parts) == 2 {
			key = parts[1]
		}

		switch r.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			store[key] = body
			modTimes[key] = time.Now().UTC()
			mu.Unlock()
			w.Header().Set("ETag", `"mock-etag"`)
			w.WriteHeader(http.StatusOK)

		case http.MethodGet:
			if r.URL.Query().Get("list-type") == "2" {
				prefix := r.URL.Query().Get("prefix")
				var sb strings.Builder
				sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult><IsTruncated>false</IsTruncated>`)
				mu.RLock()
				for k, v := range store {
					if strings.HasPrefix(k, prefix) {
						mt := modTimes[k].Format(time.RFC3339)
						sb.WriteString(fmt.Sprintf(`<Contents><Key>%s</Key><LastModified>%s</LastModified><Size>%d</Size></Contents>`, k, mt, len(v)))
					}
				}
				mu.RUnlock()
				sb.WriteString(`</ListBucketResult>`)
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(sb.String()))
				return
			}
			mu.RLock()
			data, ok := store[key]
			mu.RUnlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)

		case http.MethodHead:
			mu.RLock()
			data, ok := store[key]
			mu.RUnlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
			w.WriteHeader(http.StatusOK)

		case http.MethodDelete:
			mu.Lock()
			delete(store, key)
			delete(modTimes, key)
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer s3Srv.Close()

	// Configure app to use the S3-compatible filesystem driver
	app.Settings().S3.Enabled = true
	app.Settings().S3.Bucket = "test-bucket"
	app.Settings().S3.Region = "us-east-1"
	app.Settings().S3.Endpoint = s3Srv.URL
	app.Settings().S3.AccessKey = "test-ak"
	app.Settings().S3.Secret = "test-sk"
	app.Settings().S3.ForcePathStyle = true

	sm := app.Storage()
	bucketName := "s3-multipart-bucket"
	_, err = sm.CreateBucket(&core.StorageBucket{
		Name:          bucketName,
		Public:        false,
		FileSizeLimit: 10 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}

	ctx := context.Background()

	// 1. Multipart upload + crash after metadata commit + session-bound recovery on S3 backend
	sess, err := sm.StartMultipartUpload(bucketName, "s3_recovered.bin", "tenant_s3")
	if err != nil {
		t.Fatalf("StartMultipartUpload failed: %v", err)
	}
	p1 := []byte("s3-chunk-one-")
	p2 := []byte("s3-chunk-two")
	if _, err := sm.UploadPart(sess.Id, 1, p1); err != nil {
		t.Fatalf("UploadPart 1 on S3 failed: %v", err)
	}
	if _, err := sm.UploadPart(sess.Id, 2, p2); err != nil {
		t.Fatalf("UploadPart 2 on S3 failed: %v", err)
	}

	core.SetStorageFaultHooksForTest(sm, &core.StorageFaultHooksForTest{
		AfterMetadataCommit: func(uploadId string, obj *core.StorageObject) error {
			return errors.New("simulated crash before S3 status ack")
		},
	})
	if _, err := sm.CompleteMultipartUploadWithLease(ctx, sess.Id, 100*time.Millisecond); err == nil {
		t.Fatalf("expected crash error on S3 completion")
	}
	core.SetStorageFaultHooksForTest(sm, nil)
	time.Sleep(150 * time.Millisecond)

	recObj, err := sm.CompleteMultipartUpload(ctx, sess.Id)
	if err != nil {
		t.Fatalf("S3 session-bound recovery failed: %v", err)
	}
	if recObj.UploadId != sess.Id {
		t.Fatalf("expected S3 recovered upload_id=%q, got %q", sess.Id, recObj.UploadId)
	}

	_, gotData, err := sm.GetObject(ctx, bucketName, "s3_recovered.bin", "tenant_s3", false)
	if err != nil || string(gotData) != "s3-chunk-one-s3-chunk-two" {
		t.Fatalf("S3 GetObject mismatch: data=%q err=%v", string(gotData), err)
	}

	// 2. Two-stage orphan GC on S3 backend
	staleSess, _ := sm.StartMultipartUpload(bucketName, "s3_stale.bin", "tenant_s3")
	stalePart, err := sm.UploadPart(staleSess.Id, 1, []byte("stale-s3-chunk"))
	if err != nil {
		t.Fatalf("UploadPart on stale S3 session failed: %v", err)
	}
	_, _ = app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[updated]] = now() - INTERVAL '48 hours' WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": staleSess.Id}).Execute()

	purged, err := sm.CleanupOrphanedUploads(ctx, 24*time.Hour)
	if err != nil || purged < 1 {
		t.Fatalf("S3 CleanupOrphanedUploads failed: purged=%d err=%v", purged, err)
	}

	mu.RLock()
	_, stillExists := store[stalePart.StorageKey]
	mu.RUnlock()
	if stillExists {
		t.Fatalf("expected stale S3 part %s to be deleted by GC", stalePart.StorageKey)
	}
}
