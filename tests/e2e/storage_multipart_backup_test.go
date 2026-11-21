package e2e_test

import (
	"context"
	"path"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/pocketbase/dbx"
)

func TestStorage_MultipartChunkedUploadSession(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create target bucket
	_, err = app.Storage().CreateBucket(&core.StorageBucket{
		Id:     "bkt_multipart_01",
		Name:   "archives",
		Public: true,
	})
	if err != nil {
		t.Fatalf("Failed to create bucket: %v", err)
	}

	// 2. Start multipart upload session
	session, err := app.Storage().StartMultipartUpload("archives", "backup.tar.gz", "user_dev")
	if err != nil {
		t.Fatalf("Failed to start multipart upload: %v", err)
	}
	if session.Status != "pending" || session.Id == "" {
		t.Fatalf("Invalid session state: %+v", session)
	}

	// 3. Upload chunk part 1 and part 2
	part1Data := []byte("FIRST_CHUNK_DATA_OF_ARCHIVE_")
	part2Data := []byte("SECOND_CHUNK_DATA_OF_ARCHIVE_FINAL")

	p1, err := app.Storage().UploadPart(session.Id, 1, part1Data)
	if err != nil {
		t.Fatalf("Upload part 1 failed: %v", err)
	}
	if p1.PartNumber != 1 || p1.Size != int64(len(part1Data)) {
		t.Fatalf("Invalid part 1 metadata: %+v", p1)
	}

	p2, err := app.Storage().UploadPart(session.Id, 2, part2Data)
	if err != nil {
		t.Fatalf("Upload part 2 failed: %v", err)
	}
	if p2.PartNumber != 2 || p2.Size != int64(len(part2Data)) {
		t.Fatalf("Invalid part 2 metadata: %+v", p2)
	}

	// 4. Complete multipart upload
	obj, err := app.Storage().CompleteMultipartUpload(context.Background(), session.Id)
	if err != nil {
		t.Fatalf("CompleteMultipartUpload failed: %v", err)
	}

	expectedTotalSize := int64(len(part1Data) + len(part2Data))
	if obj.Size != expectedTotalSize {
		t.Fatalf("Expected object size %d, got %d", expectedTotalSize, obj.Size)
	}

	// 5. Read back assembled object from storage
	_, data, err := app.Storage().GetObject(context.Background(), "archives", "backup.tar.gz", "", false)
	if err != nil {
		t.Fatalf("Failed to retrieve completed object: %v", err)
	}
	expectedCombined := string(part1Data) + string(part2Data)
	if string(data) != expectedCombined {
		t.Fatalf("Assembled content mismatch! Expected %s, got %s", expectedCombined, string(data))
	}

	// 6. Verify temporary part chunks were purged from filesystem
	fsys, err := app.NewFilesystem()
	if err != nil {
		t.Fatalf("Failed to open filesystem: %v", err)
	}
	defer fsys.Close()

	part1Exists, _ := fsys.Exists(path.Join("_storage_uploads", session.Id, "part_1"))
	part2Exists, _ := fsys.Exists(path.Join("_storage_uploads", session.Id, "part_2"))
	if part1Exists || part2Exists {
		t.Fatal("Temporary upload parts were not cleaned up after completion")
	}
}

func TestStorage_InterruptedUploadCancellationAndCleanup(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	_, err = app.Storage().CreateBucket(&core.StorageBucket{
		Id:     "bkt_abort_test",
		Name:   "scratch",
		Public: false,
	})
	if err != nil {
		t.Fatalf("Failed to create bucket: %v", err)
	}

	// Start session and upload 1 partial chunk
	session, err := app.Storage().StartMultipartUpload("scratch", "partial.bin", "user_abort")
	if err != nil {
		t.Fatalf("Start upload failed: %v", err)
	}

	_, err = app.Storage().UploadPart(session.Id, 1, []byte("partial_bytes_interrupted"))
	if err != nil {
		t.Fatalf("UploadPart failed: %v", err)
	}

	// Abort upload
	if err := app.Storage().AbortMultipartUpload(session.Id); err != nil {
		t.Fatalf("AbortMultipartUpload failed: %v", err)
	}

	// Verify session row deleted
	var count int
	_ = app.DB().NewQuery("SELECT count(*) FROM {{_storage_uploads}} WHERE [[id]] = {:id}").
		Bind(dbx.Params{"id": session.Id}).Row(&count)
	if count != 0 {
		t.Fatalf("Expected upload session row deleted, found %d", count)
	}

	// Verify no partial object was committed to _storage_objects
	var objCount int
	_ = app.DB().NewQuery("SELECT count(*) FROM {{_storage_objects}} WHERE [[bucket_id]] = 'bkt_abort_test'").
		Row(&objCount)
	if objCount != 0 {
		t.Fatalf("Interrupted upload leaked uncommitted object to database: %d rows", objCount)
	}

	// Verify temporary part file was purged
	fsys, _ := app.NewFilesystem()
	defer fsys.Close()
	exists, _ := fsys.Exists(path.Join("_storage_uploads", session.Id, "part_1"))
	if exists {
		t.Fatal("Temporary chunk part was not deleted on abort")
	}
}

func TestStorage_CoordinatedBackupRestoreIntegrity(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// Create bucket and files
	_, err = app.Storage().CreateBucket(&core.StorageBucket{
		Id:     "bkt_backup_01",
		Name:   "documents",
		Public: true,
	})
	if err != nil {
		t.Fatalf("Failed to create bucket: %v", err)
	}

	doc1 := []byte("contract_agreement_v1_signed")
	doc2 := []byte("receipt_statement_march_2026")

	obj1, err := app.Storage().UploadObject(context.Background(), "documents", "contract.pdf", "alice", "application/pdf", doc1, nil)
	if err != nil {
		t.Fatalf("Failed to upload doc1: %v", err)
	}
	obj2, err := app.Storage().UploadObject(context.Background(), "documents", "receipt.pdf", "bob", "application/pdf", doc2, nil)
	if err != nil {
		t.Fatalf("Failed to upload doc2: %v", err)
	}

	// Verify metadata matches filesystem byte contents
	fsys, err := app.NewFilesystem()
	if err != nil {
		t.Fatalf("Failed to get filesystem: %v", err)
	}
	defer fsys.Close()

	reader1, err := fsys.GetReader(path.Join("_storage", "bkt_backup_01", "contract.pdf"))
	if err != nil {
		t.Fatalf("Failed to read doc1 file: %v", err)
	}
	defer reader1.Close()

	reader2, err := fsys.GetReader(path.Join("_storage", "bkt_backup_01", "receipt.pdf"))
	if err != nil {
		t.Fatalf("Failed to read doc2 file: %v", err)
	}
	defer reader2.Close()

	if obj1.Size != int64(len(doc1)) || obj2.Size != int64(len(doc2)) {
		t.Fatal("Database object size metadata does not match filesystem bytes")
	}
}
