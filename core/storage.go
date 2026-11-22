package core

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AbdullahZaher/ruxbase/tools/filesystem"
	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/AbdullahZaher/ruxbase/tools/types"
	"github.com/pocketbase/dbx"
)

var (
	ErrStorageBucketNotFound        = errors.New("storage bucket not found")
	ErrStorageObjectNotFound        = errors.New("storage object not found")
	ErrStorageAccessDenied          = errors.New("storage access denied: cross-tenant or private object")
	ErrStorageFileSizeExceeded      = errors.New("storage error: file size exceeds bucket limit")
	ErrStorageInvalidMimeType       = errors.New("storage error: MIME type not allowed in bucket")
	ErrStorageInvalidSignedToken    = errors.New("storage error: invalid or expired signed URL token")
	ErrStorageObjectConflict        = errors.New("storage error: target object already exists from a different session")
	ErrStorageUploadInProgress      = errors.New("storage error: upload session is currently being completed")
	ErrStorageUploadAlreadyComplete = errors.New("storage error: upload session is already completed")
	ErrStorageUploadExpired         = errors.New("storage error: upload session has expired")
	ErrStorageIntegrityMismatch     = errors.New("storage error: stored object integrity verification failed")
)

// StorageBucket represents a storage bucket container.
type StorageBucket struct {
	Id               string                  `db:"id" json:"id"`
	Name             string                  `db:"name" json:"name"`
	Public           bool                    `db:"public" json:"public"`
	FileSizeLimit    int64                   `db:"file_size_limit" json:"fileSizeLimit"`
	AllowedMimeTypes types.JSONArray[string] `db:"allowed_mime_types" json:"allowedMimeTypes,omitempty"`
	Created          types.DateTime          `db:"created" json:"created"`
	Updated          types.DateTime          `db:"updated" json:"updated"`
}

// StorageObject represents an object/file stored in a bucket.
type StorageObject struct {
	Id              string             `db:"id" json:"id"`
	BucketId        string             `db:"bucket_id" json:"bucketId"`
	Name            string             `db:"name" json:"name"`
	OwnerId         string             `db:"owner_id" json:"ownerId"`
	Size            int64              `db:"size" json:"size"`
	MimeType        string             `db:"mime_type" json:"mimeType"`
	Etag            string             `db:"etag" json:"etag"`
	Metadata        types.JSONMap[any] `db:"metadata" json:"metadata,omitempty"`
	UploadId        string             `db:"upload_id" json:"uploadId,omitempty"`
	CompletionToken string             `db:"completion_token" json:"completionToken,omitempty"`
	Created         types.DateTime     `db:"created" json:"created"`
	Updated         types.DateTime     `db:"updated" json:"updated"`
}

// StorageUploadPart represents a single part of a multipart upload.
type StorageUploadPart struct {
	PartNumber int    `json:"partNumber"`
	Size       int64  `json:"size"`
	Etag       string `json:"etag"`
	StorageKey string `json:"storageKey,omitempty"`
}

// StorageUploadSession represents a multi-part resumable upload session.
type StorageUploadSession struct {
	Id                    string                             `db:"id" json:"id"`
	BucketId              string                             `db:"bucket_id" json:"bucketId"`
	Name                  string                             `db:"name" json:"name"`
	OwnerId               string                             `db:"owner_id" json:"ownerId"`
	Parts                 types.JSONArray[StorageUploadPart] `db:"parts" json:"parts"`
	TotalSize             int64                              `db:"total_size" json:"totalSize"`
	Status                string                             `db:"status" json:"status"`
	CompletionToken       string                             `db:"completion_token" json:"completionToken,omitempty"`
	CompletionLeasedUntil types.DateTime                     `db:"completion_leased_until" json:"completionLeasedUntil,omitempty"`
	ExpectedSha256        string                             `db:"expected_sha256" json:"expectedSha256,omitempty"`
	CompletedObjectId     string                             `db:"completed_object_id" json:"completedObjectId,omitempty"`
	Created               types.DateTime                     `db:"created" json:"created"`
	Updated               types.DateTime                     `db:"updated" json:"updated"`
}

// storageFaultHooks provides deterministic test-only fault injection and synchronization barriers.
type storageFaultHooks struct {
	beforePartCommit     func(uploadId string, partNumber int, partKey string) error
	afterCompletionClaim func(uploadId string, completionToken string) error
	beforeFinalBlobWrite func(uploadId string, fileKey string) error
	afterFinalBlobWrite  func(uploadId string, fileKey string) error
	afterMetadataCommit  func(uploadId string, obj *StorageObject) error
	beforeGCStageA       func() error
	afterGCStageA        func(expiredUploadIds []string) error
	beforeGCDeleteKey    func(storageKey string) error
}

// StorageManager manages storage buckets, objects, and permissions.
type StorageManager struct {
	app        App
	hooksMu    sync.RWMutex
	faultHooks *storageFaultHooks
}

// NewStorageManager creates a new StorageManager instance.
func NewStorageManager(app App) *StorageManager {
	return &StorageManager{app: app}
}

func (sm *StorageManager) getFaultHooks() *storageFaultHooks {
	sm.hooksMu.RLock()
	defer sm.hooksMu.RUnlock()
	return sm.faultHooks
}

func (sm *StorageManager) objectKey(bucketId, objectName string) string {
	cleanName := strings.TrimLeft(path.Clean(objectName), "/")
	return path.Join("_storage", bucketId, cleanName)
}

func (sm *StorageManager) uploadPartKey(uploadId string, partNumber int) string {
	return path.Join("_storage_uploads", uploadId, fmt.Sprintf("part_%d", partNumber))
}

// CreateBucket creates a new storage bucket.
func (sm *StorageManager) CreateBucket(bucket *StorageBucket) (*StorageBucket, error) {
	if bucket == nil {
		return nil, errors.New("cannot create nil bucket")
	}
	if bucket.Name == "" {
		return nil, errors.New("bucket name is required")
	}
	if bucket.Id == "" {
		bucket.Id = security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
	}
	if bucket.FileSizeLimit <= 0 {
		bucket.FileSizeLimit = 10 * 1024 * 1024 // 10MB default
	}
	now := types.NowDateTime()
	bucket.Created = now
	bucket.Updated = now

	mimesBytes, _ := json.Marshal(bucket.AllowedMimeTypes)

	_, err := sm.app.DB().NewQuery(`
		INSERT INTO {{_storage_buckets}} ([[id]], [[name]], [[public]], [[file_size_limit]], [[allowed_mime_types]], [[created]], [[updated]])
		VALUES ({:id}, {:name}, {:public}, {:limit}, {:mimes}::jsonb, {:created}, {:updated})
	`).Bind(dbx.Params{
		"id":      bucket.Id,
		"name":    bucket.Name,
		"public":  bucket.Public,
		"limit":   bucket.FileSizeLimit,
		"mimes":   string(mimesBytes),
		"created": bucket.Created.String(),
		"updated": bucket.Updated.String(),
	}).Execute()

	if err != nil {
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	return bucket, nil
}

// GetBucket retrieves a bucket by ID or name.
func (sm *StorageManager) GetBucket(nameOrId string) (*StorageBucket, error) {
	b := &StorageBucket{}
	err := sm.app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[public]], [[file_size_limit]], [[allowed_mime_types]], [[created]], [[updated]]
		FROM {{_storage_buckets}}
		WHERE [[id]] = {:val} OR [[name]] = {:val}
		LIMIT 1
	`).Bind(dbx.Params{"val": nameOrId}).One(b)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStorageBucketNotFound
		}
		return nil, err
	}
	return b, nil
}

// DeleteBucket deletes a bucket and all its contained objects.
func (sm *StorageManager) DeleteBucket(nameOrId string) error {
	b, err := sm.GetBucket(nameOrId)
	if err != nil {
		return err
	}

	// Delete all filesystem files under bucket prefix
	fsys, err := sm.app.NewFilesystem()
	if err == nil {
		defer fsys.Close()
		_ = fsys.DeletePrefix(path.Join("_storage", b.Id))
	}

	_, err = sm.app.DB().NewQuery(`
		DELETE FROM {{_storage_buckets}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": b.Id}).Execute()

	return err
}

// UploadObject stores a file in the bucket after enforcing quotas, ownership, and MIME restrictions.
func (sm *StorageManager) UploadObject(
	ctx context.Context,
	bucketNameOrId string,
	objectName string,
	ownerId string,
	mimeType string,
	data []byte,
	metadata map[string]any,
) (*StorageObject, error) {
	b, err := sm.GetBucket(bucketNameOrId)
	if err != nil {
		return nil, err
	}

	// 1. Quota check: file size limit
	if int64(len(data)) > b.FileSizeLimit {
		return nil, ErrStorageFileSizeExceeded
	}

	// 2. MIME type check if configured
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	if len(b.AllowedMimeTypes) > 0 {
		allowed := false
		for _, m := range b.AllowedMimeTypes {
			if m == mimeType || m == "*/*" || strings.HasPrefix(mimeType, strings.TrimSuffix(m, "*")) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, ErrStorageInvalidMimeType
		}
	}

	// 3. Guard against racing an actively completing multipart upload on the exact same target key
	var multipartCompleting bool
	if err := sm.app.DB().NewQuery(`
		SELECT EXISTS (
			SELECT 1 FROM {{_storage_uploads}}
			WHERE [[bucket_id]] = {:bucketId}
			  AND [[name]] = {:name}
			  AND [[status]] = 'completing'
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"bucketId": b.Id,
		"name":     objectName,
	}).Row(&multipartCompleting); err != nil {
		return nil, fmt.Errorf("failed to check active multipart completion: %w", err)
	}
	if multipartCompleting {
		return nil, fmt.Errorf("%w: cannot overwrite %s/%s while multipart completion is in flight", ErrStorageUploadInProgress, b.Id, objectName)
	}

	// 4. Compute ETag (SHA-256)
	hash := sha256.Sum256(data)
	etag := hex.EncodeToString(hash[:])

	// 4. Write blob to filesystem
	fsys, err := sm.app.NewFilesystem()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize filesystem: %w", err)
	}
	defer fsys.Close()

	fileKey := sm.objectKey(b.Id, objectName)
	if err := fsys.Upload(data, fileKey); err != nil {
		return nil, fmt.Errorf("failed to write object to storage: %w", err)
	}

	// 5. Upsert metadata in PostgreSQL
	objId := security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
	now := types.NowDateTime()
	metaBytes, _ := json.Marshal(metadata)

	obj := &StorageObject{}
	err = sm.app.DB().NewQuery(`
		INSERT INTO {{_storage_objects}} (
			[[id]], [[bucket_id]], [[name]], [[owner_id]], [[size]], [[mime_type]],
			[[etag]], [[metadata]], [[upload_id]], [[completion_token]], [[created]], [[updated]]
		) VALUES (
			{:id}, {:bucketId}, {:name}, {:ownerId}, {:size}, {:mimeType},
			{:etag}, {:metadata}::jsonb, '', '', {:created}, {:updated}
		)
		ON CONFLICT ([[bucket_id]], [[name]]) DO UPDATE SET
			[[owner_id]] = EXCLUDED.[[owner_id]],
			[[size]] = EXCLUDED.[[size]],
			[[mime_type]] = EXCLUDED.[[mime_type]],
			[[etag]] = EXCLUDED.[[etag]],
			[[metadata]] = EXCLUDED.[[metadata]],
			[[upload_id]] = '',
			[[completion_token]] = '',
			[[updated]] = now()
		RETURNING [[id]], [[bucket_id]], [[name]], [[owner_id]], [[size]], [[mime_type]],
		          [[etag]], [[metadata]], [[upload_id]], [[completion_token]], [[created]], [[updated]]
	`).Bind(dbx.Params{
		"id":       objId,
		"bucketId": b.Id,
		"name":     objectName,
		"ownerId":  ownerId,
		"size":     int64(len(data)),
		"mimeType": mimeType,
		"etag":     etag,
		"metadata": string(metaBytes),
		"created":  now.String(),
		"updated":  now.String(),
	}).One(obj)

	if err != nil {
		return nil, fmt.Errorf("failed to record object metadata: %w", err)
	}

	return obj, nil
}

// GetObject retrieves object metadata and data with authorization enforcement.
func (sm *StorageManager) GetObject(
	ctx context.Context,
	bucketNameOrId string,
	objectName string,
	callerId string,
	isSuperuser bool,
) (*StorageObject, []byte, error) {
	b, err := sm.GetBucket(bucketNameOrId)
	if err != nil {
		return nil, nil, err
	}

	obj := &StorageObject{}
	err = sm.app.DB().NewQuery(`
		SELECT [[id]], [[bucket_id]], [[name]], [[owner_id]], [[size]], [[mime_type]],
		       [[etag]], [[metadata]], [[upload_id]], [[completion_token]], [[created]], [[updated]]
		FROM {{_storage_objects}}
		WHERE [[bucket_id]] = {:bucketId} AND [[name]] = {:name}
	`).Bind(dbx.Params{
		"bucketId": b.Id,
		"name":     objectName,
	}).One(obj)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrStorageObjectNotFound
		}
		return nil, nil, err
	}

	// Authorization check
	if !b.Public && !isSuperuser {
		if callerId == "" || callerId != obj.OwnerId {
			return nil, nil, ErrStorageAccessDenied
		}
	}

	// Read file from filesystem
	fsys, err := sm.app.NewFilesystem()
	if err != nil {
		return nil, nil, err
	}
	defer fsys.Close()

	fileKey := sm.objectKey(b.Id, objectName)
	reader, err := fsys.GetReader(fileKey)
	if err != nil {
		return nil, nil, ErrStorageObjectNotFound
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read object content: %w", err)
	}

	return obj, data, nil
}

// DeleteObject deletes an object if the caller is the owner or a superuser.
func (sm *StorageManager) DeleteObject(
	ctx context.Context,
	bucketNameOrId string,
	objectName string,
	callerId string,
	isSuperuser bool,
) error {
	b, err := sm.GetBucket(bucketNameOrId)
	if err != nil {
		return err
	}

	obj := &StorageObject{}
	err = sm.app.DB().NewQuery(`
		SELECT [[id]], [[bucket_id]], [[name]], [[owner_id]]
		FROM {{_storage_objects}}
		WHERE [[bucket_id]] = {:bucketId} AND [[name]] = {:name}
	`).Bind(dbx.Params{
		"bucketId": b.Id,
		"name":     objectName,
	}).One(obj)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrStorageObjectNotFound
		}
		return err
	}

	if !isSuperuser && (callerId == "" || callerId != obj.OwnerId) {
		return ErrStorageAccessDenied
	}

	// Delete from filesystem
	fsys, err := sm.app.NewFilesystem()
	if err == nil {
		defer fsys.Close()
		_ = fsys.Delete(sm.objectKey(b.Id, objectName))
	}

	_, err = sm.app.DB().NewQuery(`
		DELETE FROM {{_storage_objects}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": obj.Id}).Execute()

	return err
}

// CreateSignedURL creates an HMAC-SHA256 signed download token for temporary object access.
func (sm *StorageManager) CreateSignedURL(bucketName, objectName string, ttl time.Duration, secret string) (string, error) {
	if secret == "" {
		secret = "ruxbase_storage_default_secret"
	}
	expiry := time.Now().Add(ttl).Unix()
	payload := fmt.Sprintf("%s:%s:%d", bucketName, objectName, expiry)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	tokenRaw := fmt.Sprintf("%s:%s", payload, sig)
	return base64.URLEncoding.EncodeToString([]byte(tokenRaw)), nil
}

// VerifySignedToken validates an HMAC-SHA256 signed URL token and returns bucket and object names.
func (sm *StorageManager) VerifySignedToken(token string, secret string) (string, string, error) {
	if secret == "" {
		secret = "ruxbase_storage_default_secret"
	}
	rawBytes, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return "", "", ErrStorageInvalidSignedToken
	}

	parts := strings.Split(string(rawBytes), ":")
	if len(parts) != 4 {
		return "", "", ErrStorageInvalidSignedToken
	}

	bucketName := parts[0]
	objectName := parts[1]
	expiryStr := parts[2]
	providedSig := parts[3]

	expiry, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return "", "", ErrStorageInvalidSignedToken
	}

	payload := fmt.Sprintf("%s:%s:%d", bucketName, objectName, expiry)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(providedSig), []byte(expectedSig)) {
		return "", "", ErrStorageInvalidSignedToken
	}

	return bucketName, objectName, nil
}

// ReconcileOrphans detects and purges filesystem blobs lacking corresponding database records.
func (sm *StorageManager) ReconcileOrphans(ctx context.Context, bucketNameOrId string) (int, error) {
	b, err := sm.GetBucket(bucketNameOrId)
	if err != nil {
		return 0, err
	}

	fsys, err := sm.app.NewFilesystem()
	if err != nil {
		return 0, err
	}
	defer fsys.Close()

	prefix := path.Join("_storage", b.Id)
	items, err := fsys.List(prefix)
	if err != nil {
		return 0, err
	}

	orphansPurged := 0
	for _, item := range items {
		// Key format: _storage/<bucketId>/<objectName>
		relName := strings.TrimPrefix(item.Key, prefix+"/")
		var exists bool
		_ = sm.app.DB().NewQuery(`
			SELECT EXISTS (
				SELECT 1 FROM {{_storage_objects}}
				WHERE [[bucket_id]] = {:bucketId} AND [[name]] = {:name}
			)
		`).Bind(dbx.Params{
			"bucketId": b.Id,
			"name":     relName,
		}).Row(&exists)

		if !exists {
			_ = fsys.Delete(item.Key)
			orphansPurged++
		}
	}

	return orphansPurged, nil
}

// StartMultipartUpload initializes a chunked multipart upload session.
func (sm *StorageManager) StartMultipartUpload(bucketNameOrId, objectName, ownerId string) (*StorageUploadSession, error) {
	b, err := sm.GetBucket(bucketNameOrId)
	if err != nil {
		return nil, err
	}

	session := &StorageUploadSession{
		Id:        security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789"),
		BucketId:  b.Id,
		Name:      objectName,
		OwnerId:   ownerId,
		Parts:     make(types.JSONArray[StorageUploadPart], 0),
		TotalSize: 0,
		Status:    "pending",
		Created:   types.NowDateTime(),
		Updated:   types.NowDateTime(),
	}

	partsBytes, _ := json.Marshal(session.Parts)

	_, err = sm.app.DB().NewQuery(`
		INSERT INTO {{_storage_uploads}} (
			[[id]], [[bucket_id]], [[name]], [[owner_id]], [[parts]], [[total_size]], [[status]], [[created]], [[updated]]
		) VALUES (
			{:id}, {:bucketId}, {:name}, {:ownerId}, {:parts}::jsonb, {:totalSize}, {:status}, {:created}, {:updated}
		)
	`).Bind(dbx.Params{
		"id":        session.Id,
		"bucketId":  session.BucketId,
		"name":      session.Name,
		"ownerId":   session.OwnerId,
		"parts":     string(partsBytes),
		"totalSize": session.TotalSize,
		"status":    session.Status,
		"created":   session.Created.String(),
		"updated":   session.Updated.String(),
	}).Execute()

	if err != nil {
		return nil, fmt.Errorf("failed to start multipart upload: %w", err)
	}

	return session, nil
}

func (sm *StorageManager) enqueueCleanupKeyTx(txApp App, uploadId, storageKey, reason string) error {
	if storageKey == "" {
		return nil
	}
	taskId := security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
	_, err := txApp.DB().NewQuery(`
		INSERT INTO {{_storage_upload_cleanup}} ([[id]], [[upload_id]], [[storage_key]], [[reason]], [[attempts]], [[last_error]], [[created]], [[updated]])
		VALUES ({:id}, {:uploadId}, {:storageKey}, {:reason}, 0, '', now(), now())
		ON CONFLICT ([[storage_key]]) DO NOTHING
	`).Bind(dbx.Params{
		"id":         taskId,
		"uploadId":   uploadId,
		"storageKey": storageKey,
		"reason":     reason,
	}).Execute()
	return err
}

func (sm *StorageManager) deleteFileAndDequeueCleanup(fsys *filesystem.System, storageKey string) error {
	if storageKey == "" {
		return nil
	}
	if hooks := sm.getFaultHooks(); hooks != nil && hooks.beforeGCDeleteKey != nil {
		if err := hooks.beforeGCDeleteKey(storageKey); err != nil {
			_, _ = sm.app.DB().NewQuery(`
				UPDATE {{_storage_upload_cleanup}}
				SET [[attempts]] = [[attempts]] + 1,
				    [[last_error]] = {:err},
				    [[updated]] = now()
				WHERE [[storage_key]] = {:storageKey}
			`).Bind(dbx.Params{"storageKey": storageKey, "err": err.Error()}).Execute()
			return err
		}
	}

	delErr := fsys.Delete(storageKey)
	if delErr != nil && !errors.Is(delErr, filesystem.ErrNotFound) {
		_, _ = sm.app.DB().NewQuery(`
			UPDATE {{_storage_upload_cleanup}}
			SET [[attempts]] = [[attempts]] + 1,
			    [[last_error]] = {:err},
			    [[updated]] = now()
			WHERE [[storage_key]] = {:storageKey}
		`).Bind(dbx.Params{"storageKey": storageKey, "err": delErr.Error()}).Execute()
		return delErr
	}

	_, _ = sm.app.DB().NewQuery(`
		DELETE FROM {{_storage_upload_cleanup}} WHERE [[storage_key]] = {:storageKey}
	`).Bind(dbx.Params{"storageKey": storageKey}).Execute()
	return nil
}

func (sm *StorageManager) verifyStoredBlobIntegrity(fsys *filesystem.System, fileKey string, expectedSize int64, expectedSha256 string) error {
	reader, err := fsys.GetReader(fileKey)
	if err != nil {
		return fmt.Errorf("%w: failed to open stored blob %s: %v", ErrStorageIntegrityMismatch, fileKey, err)
	}
	defer reader.Close()

	hasher := sha256.New()
	written, err := io.Copy(hasher, reader)
	if err != nil {
		return fmt.Errorf("%w: failed to read stored blob %s: %v", ErrStorageIntegrityMismatch, fileKey, err)
	}
	if expectedSize >= 0 && written != expectedSize {
		return fmt.Errorf("%w: stored blob size mismatch for %s (expected %d, got %d)", ErrStorageIntegrityMismatch, fileKey, expectedSize, written)
	}
	actualSha := hex.EncodeToString(hasher.Sum(nil))
	if expectedSha256 != "" && actualSha != expectedSha256 {
		return fmt.Errorf("%w: stored blob sha256 mismatch for %s (expected %s, got %s)", ErrStorageIntegrityMismatch, fileKey, expectedSha256, actualSha)
	}
	return nil
}

func (sm *StorageManager) revertCompletingToPending(uploadId, completionToken string) {
	_, _ = sm.app.DB().NewQuery(`
		UPDATE {{_storage_uploads}}
		SET [[status]] = 'pending',
		    [[completion_token]] = '',
		    [[completion_leased_until]] = NULL,
		    [[expected_sha256]] = '',
		    [[updated]] = now()
		WHERE [[id]] = {:id}
		  AND [[status]] = 'completing'
		  AND [[completion_token]] = {:token}
	`).Bind(dbx.Params{
		"id":    uploadId,
		"token": completionToken,
	}).Execute()
}

func isUniqueViolationErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") || strings.Contains(msg, "duplicate key value violates unique constraint")
}

// UploadPart writes a single chunk part for an active multipart upload session.
// It uses immutable per-attempt temporary file keys to prevent filesystem races and
// row locking inside a database transaction to prevent concurrency races on part metadata.
func (sm *StorageManager) UploadPart(uploadId string, partNumber int, data []byte) (*StorageUploadPart, error) {
	if partNumber < 1 || partNumber > 10000 {
		return nil, fmt.Errorf("invalid part number %d: must be between 1 and 10000", partNumber)
	}

	fsys, err := sm.app.NewFilesystem()
	if err != nil {
		return nil, err
	}
	defer fsys.Close()

	// Immutable per-attempt temporary storage key prevents concurrent uploads
	// of the same part number from overwriting each other's filesystem contents.
	attemptToken := security.RandomStringWithAlphabet(16, "abcdefghijklmnopqrstuvwxyz0123456789")
	partKey := fmt.Sprintf("_storage_uploads/%s/part_%d_%s", uploadId, partNumber, attemptToken)
	if err := fsys.Upload(data, partKey); err != nil {
		return nil, fmt.Errorf("failed to upload part %d: %w", partNumber, err)
	}

	if hooks := sm.getFaultHooks(); hooks != nil && hooks.beforePartCommit != nil {
		if hookErr := hooks.beforePartCommit(uploadId, partNumber, partKey); hookErr != nil {
			_ = fsys.Delete(partKey)
			return nil, hookErr
		}
	}

	hash := sha256.Sum256(data)
	etag := hex.EncodeToString(hash[:])

	part := StorageUploadPart{
		PartNumber: partNumber,
		Size:       int64(len(data)),
		Etag:       etag,
		StorageKey: partKey,
	}

	var oldKeyToDelete string

	// Update session parts atomically with row lock
	err = sm.app.RunInTransaction(func(txApp App) error {
		session := &StorageUploadSession{}
		err := txApp.DB().NewQuery(`
			SELECT [[id]], [[bucket_id]], [[name]], [[owner_id]], [[parts]], [[total_size]], [[status]], [[created]], [[updated]]
			FROM {{_storage_uploads}}
			WHERE [[id]] = {:id}
			FOR UPDATE
		`).Bind(dbx.Params{"id": uploadId}).One(session)
		if err != nil {
			return fmt.Errorf("upload session not found: %w", err)
		}
		if session.Status == "expired" {
			return fmt.Errorf("%w: upload session %s", ErrStorageUploadExpired, uploadId)
		}
		if session.Status != "pending" {
			return fmt.Errorf("upload session is not pending (status: %s)", session.Status)
		}

		// Upsert part: replace if already exists, else append
		replaced := false
		for i, p := range session.Parts {
			if p.PartNumber == partNumber {
				oldKeyToDelete = p.StorageKey
				if oldKeyToDelete == "" {
					oldKeyToDelete = sm.uploadPartKey(uploadId, partNumber)
				}
				session.Parts[i] = part
				replaced = true
				break
			}
		}
		if !replaced {
			session.Parts = append(session.Parts, part)
		}

		// Sort parts by PartNumber
		slices.SortFunc(session.Parts, func(a, b StorageUploadPart) int {
			return cmp.Compare(a.PartNumber, b.PartNumber)
		})

		// Recalculate TotalSize accurately as the sum of all distinct parts
		var totalSize int64
		for _, p := range session.Parts {
			totalSize += p.Size
		}
		session.TotalSize = totalSize

		partsBytes, err := json.Marshal(session.Parts)
		if err != nil {
			return fmt.Errorf("failed to serialize parts: %w", err)
		}

		res, err := txApp.DB().NewQuery(`
			UPDATE {{_storage_uploads}}
			SET [[parts]] = {:parts}::jsonb,
			    [[total_size]] = {:totalSize},
			    [[updated]] = now()
			WHERE [[id]] = {:id} AND [[status]] = 'pending'
		`).Bind(dbx.Params{
			"id":        uploadId,
			"parts":     string(partsBytes),
			"totalSize": session.TotalSize,
		}).Execute()
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return fmt.Errorf("upload session %s is no longer pending", uploadId)
		}

		// Durably journal replaced part key so GC can recover if post-commit deletion fails.
		if oldKeyToDelete != "" && oldKeyToDelete != partKey {
			if qErr := sm.enqueueCleanupKeyTx(txApp, uploadId, oldKeyToDelete, "replaced_part"); qErr != nil {
				return qErr
			}
		}
		return nil
	})

	if err != nil {
		if delErr := fsys.Delete(partKey); delErr != nil && !errors.Is(delErr, filesystem.ErrNotFound) {
			_ = sm.enqueueCleanupKeyTx(sm.app, uploadId, partKey, "failed_part_commit")
		}
		return nil, err
	}

	// Safely clean up the replaced older attempt's temporary file if any
	if oldKeyToDelete != "" && oldKeyToDelete != partKey {
		_ = sm.deleteFileAndDequeueCleanup(fsys, oldKeyToDelete)
	}

	return &part, nil
}

// CompleteMultipartUpload combines all uploaded parts into the final storage object
// using the default 60-second completion lease.
func (sm *StorageManager) CompleteMultipartUpload(ctx context.Context, uploadId string) (*StorageObject, error) {
	return sm.CompleteMultipartUploadWithLease(ctx, uploadId, 60*time.Second)
}

// CompleteMultipartUploadWithLease combines all uploaded parts into the final storage object.
// It enforces:
//   - Row-locked atomic state transitions (pending -> completing -> completed) with lease fencing
//   - Partial unique index serialization per target (bucket_id, name) across all processes
//   - Contiguous part validation (1..N) and per-part SHA-256 ETag verification
//   - Session-bound crash recovery verifying upload_id, expected_sha256, and actual stored blob bytes
//   - Strict rejection of unrelated preexisting objects without silent adoption or overwrite
func (sm *StorageManager) CompleteMultipartUploadWithLease(ctx context.Context, uploadId string, leaseDuration time.Duration) (*StorageObject, error) {
	if leaseDuration <= 0 {
		leaseDuration = 60 * time.Second
	}
	leaseMs := leaseDuration.Milliseconds()
	if leaseMs < 50 {
		leaseMs = 50
	}

	completionToken := security.RandomStringWithAlphabet(24, "abcdefghijklmnopqrstuvwxyz0123456789")

	var session StorageUploadSession
	var bucket *StorageBucket
	var recoverExistingObj *StorageObject

	// TX 1: Atomically inspect state, verify lease ownership / recovery eligibility, and acquire 'completing' lease.
	err := sm.app.RunInTransactionWithContext(ctx, func(txApp App) error {
		var row struct {
			StorageUploadSession
			LeaseActive bool `db:"lease_active"`
		}
		err := txApp.DB().NewQuery(`
			SELECT [[id]], [[bucket_id]], [[name]], [[owner_id]], [[parts]], [[total_size]], [[status]],
			       [[completion_token]], [[completion_leased_until]], [[expected_sha256]], [[completed_object_id]],
			       [[created]], [[updated]],
			       ([[completion_leased_until]] IS NOT NULL AND [[completion_leased_until]] >= now()) AS [[lease_active]]
			FROM {{_storage_uploads}}
			WHERE [[id]] = {:id}
			FOR UPDATE
		`).Bind(dbx.Params{"id": uploadId}).One(&row)
		if err != nil {
			return fmt.Errorf("upload session not found: %w", err)
		}
		session = row.StorageUploadSession

		switch session.Status {
		case "completed":
			return fmt.Errorf("%w: upload session %s", ErrStorageUploadAlreadyComplete, uploadId)
		case "expired":
			return fmt.Errorf("%w: upload session %s", ErrStorageUploadExpired, uploadId)
		case "completing":
			// Distinguish an active completion owner from a genuinely abandoned attempt.
			if row.LeaseActive {
				return fmt.Errorf("%w: upload session %s", ErrStorageUploadInProgress, uploadId)
			}

			// Lease expired: inspect whether this exact session already committed _storage_objects.
			existing := &StorageObject{}
			objErr := txApp.DB().NewQuery(`
				SELECT [[id]], [[bucket_id]], [[name]], [[owner_id]], [[size]], [[mime_type]],
				       [[etag]], [[metadata]], [[upload_id]], [[completion_token]], [[created]], [[updated]]
				FROM {{_storage_objects}}
				WHERE [[bucket_id]] = {:bucketId} AND [[name]] = {:name}
				FOR UPDATE
			`).Bind(dbx.Params{
				"bucketId": session.BucketId,
				"name":     session.Name,
			}).One(existing)

			if objErr == nil {
				// Strict session binding: never adopt an unrelated preexisting object or object from another session.
				if existing.UploadId != session.Id {
					return fmt.Errorf("%w: object %s/%s belongs to upload_id=%q, not %q",
						ErrStorageObjectConflict, session.BucketId, session.Name, existing.UploadId, session.Id)
				}
				if existing.OwnerId != session.OwnerId || existing.Size != session.TotalSize {
					return fmt.Errorf("%w: recovered object metadata mismatch for session %s", ErrStorageIntegrityMismatch, session.Id)
				}
				if session.CompletedObjectId != "" && existing.Id != session.CompletedObjectId {
					return fmt.Errorf("%w: recovered object id %s does not match session completed_object_id %s",
						ErrStorageIntegrityMismatch, existing.Id, session.CompletedObjectId)
				}
				if session.ExpectedSha256 != "" && existing.Etag != session.ExpectedSha256 {
					return fmt.Errorf("%w: recovered object etag %s does not match session expected_sha256 %s",
						ErrStorageIntegrityMismatch, existing.Etag, session.ExpectedSha256)
				}

				// Rotate completion lease token so no stale worker can race finalization;
				// byte-level SHA-256 verification happens outside TX 1 before marking completed.
				res, leaseErr := txApp.DB().NewQuery(`
					UPDATE {{_storage_uploads}}
					SET [[completion_token]] = {:token},
					    [[completion_leased_until]] = now() + ({:leaseMs} * INTERVAL '1 millisecond'),
					    [[updated]] = now()
					WHERE [[id]] = {:id} AND [[status]] = 'completing'
				`).Bind(dbx.Params{
					"id":      uploadId,
					"token":   completionToken,
					"leaseMs": leaseMs,
				}).Execute()
				if leaseErr != nil {
					return leaseErr
				}
				if aff, _ := res.RowsAffected(); aff == 0 {
					return fmt.Errorf("failed to acquire recovery lease for session %s", uploadId)
				}
				recoverExistingObj = existing
				return nil
			}
			if !errors.Is(objErr, sql.ErrNoRows) {
				return objErr
			}

			// No _storage_objects row committed yet (crash happened during assembly or after blob write).
			// Validate parts & bucket, rotate lease token, and fall through to verified assembly.
			b, bErr := sm.getBucketTx(txApp, session.BucketId)
			if bErr != nil {
				return bErr
			}
			bucket = b
			if err := validateContiguousParts(&session, bucket); err != nil {
				return err
			}

			res, leaseErr := txApp.DB().NewQuery(`
				UPDATE {{_storage_uploads}}
				SET [[completion_token]] = {:token},
				    [[completion_leased_until]] = now() + ({:leaseMs} * INTERVAL '1 millisecond'),
				    [[updated]] = now()
				WHERE [[id]] = {:id} AND [[status]] = 'completing'
			`).Bind(dbx.Params{
				"id":      uploadId,
				"token":   completionToken,
				"leaseMs": leaseMs,
			}).Execute()
			if leaseErr != nil {
				return leaseErr
			}
			if aff, _ := res.RowsAffected(); aff == 0 {
				return fmt.Errorf("failed to acquire recovery lease for session %s", uploadId)
			}
			return nil

		case "pending":
			b, bErr := sm.getBucketTx(txApp, session.BucketId)
			if bErr != nil {
				return bErr
			}
			bucket = b
			if err := validateContiguousParts(&session, bucket); err != nil {
				return err
			}

			// Do not silently overwrite or adopt an unrelated preexisting object.
			var existingUploadId string
			existErr := txApp.DB().NewQuery(`
				SELECT [[upload_id]]
				FROM {{_storage_objects}}
				WHERE [[bucket_id]] = {:bucketId} AND [[name]] = {:name}
				LIMIT 1
			`).Bind(dbx.Params{
				"bucketId": session.BucketId,
				"name":     session.Name,
			}).Row(&existingUploadId)
			if existErr == nil && existingUploadId != session.Id {
				return fmt.Errorf("%w: unrelated object already exists at %s/%s",
					ErrStorageObjectConflict, session.BucketId, session.Name)
			}
			if existErr != nil && !errors.Is(existErr, sql.ErrNoRows) {
				return existErr
			}

			res, upErr := txApp.DB().NewQuery(`
				UPDATE {{_storage_uploads}}
				SET [[status]] = 'completing',
				    [[completion_token]] = {:token},
				    [[completion_leased_until]] = now() + ({:leaseMs} * INTERVAL '1 millisecond'),
				    [[updated]] = now()
				WHERE [[id]] = {:id} AND [[status]] = 'pending'
			`).Bind(dbx.Params{
				"id":      uploadId,
				"token":   completionToken,
				"leaseMs": leaseMs,
			}).Execute()
			if upErr != nil {
				if isUniqueViolationErr(upErr) {
					return fmt.Errorf("%w: another upload session is currently completing %s/%s",
						ErrStorageObjectConflict, session.BucketId, session.Name)
				}
				return upErr
			}
			affected, _ := res.RowsAffected()
			if affected == 0 {
				return fmt.Errorf("failed to transition upload session %s to completing", uploadId)
			}
			return nil

		default:
			return fmt.Errorf("upload session not pending (status: %s)", session.Status)
		}
	})

	if err != nil {
		return nil, err
	}

	fsys, err := sm.app.NewFilesystem()
	if err != nil {
		if recoverExistingObj == nil {
			sm.revertCompletingToPending(uploadId, completionToken)
		}
		return nil, err
	}
	defer fsys.Close()

	// Session-bound recovery branch: object metadata already committed by this session;
	// verify actual stored blob bytes before marking session completed.
	if recoverExistingObj != nil {
		fileKey := sm.objectKey(session.BucketId, session.Name)
		expectedSha := session.ExpectedSha256
		if expectedSha == "" {
			expectedSha = recoverExistingObj.Etag
		}
		if expectedSha == "" {
			return nil, fmt.Errorf("%w: cannot verify recovered object %s without expected sha256", ErrStorageIntegrityMismatch, recoverExistingObj.Id)
		}
		if err := sm.verifyStoredBlobIntegrity(fsys, fileKey, session.TotalSize, expectedSha); err != nil {
			// Fail closed: do NOT mark session completed when stored bytes are missing or corrupted.
			return nil, err
		}

		// TX 4 (Recovery Finalization): fenced by completion_token
		if err := sm.finalizeCompletedSessionTx(ctx, &session, completionToken, recoverExistingObj.Id, expectedSha); err != nil {
			return nil, err
		}
		sm.cleanupSessionPartsBestEffort(fsys, &session)
		return recoverExistingObj, nil
	}

	if hooks := sm.getFaultHooks(); hooks != nil && hooks.afterCompletionClaim != nil {
		if hookErr := hooks.afterCompletionClaim(uploadId, completionToken); hookErr != nil {
			return nil, hookErr
		}
	}

	// Read, validate, and concatenate all parts in verified 1..N order
	var combinedData []byte
	if session.TotalSize > 0 && session.TotalSize < 2*1024*1024*1024 {
		combinedData = make([]byte, 0, session.TotalSize)
	}

	for _, part := range session.Parts {
		partKey := part.StorageKey
		if partKey == "" {
			partKey = sm.uploadPartKey(uploadId, part.PartNumber)
		}
		reader, rErr := fsys.GetReader(partKey)
		if rErr != nil {
			sm.revertCompletingToPending(uploadId, completionToken)
			return nil, fmt.Errorf("missing upload part %d: %w", part.PartNumber, rErr)
		}
		partBytes, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			sm.revertCompletingToPending(uploadId, completionToken)
			return nil, fmt.Errorf("failed to read part %d: %w", part.PartNumber, readErr)
		}
		if int64(len(partBytes)) != part.Size {
			sm.revertCompletingToPending(uploadId, completionToken)
			return nil, fmt.Errorf("part %d size mismatch: expected %d, got %d", part.PartNumber, part.Size, len(partBytes))
		}

		// Verify ETag checksum of part
		h := sha256.Sum256(partBytes)
		etag := hex.EncodeToString(h[:])
		if etag != part.Etag {
			sm.revertCompletingToPending(uploadId, completionToken)
			return nil, fmt.Errorf("part %d checksum mismatch: expected %s, got %s", part.PartNumber, part.Etag, etag)
		}

		combinedData = append(combinedData, partBytes...)
	}

	finalHash := sha256.Sum256(combinedData)
	expectedSha256 := hex.EncodeToString(finalHash[:])
	expectedSize := int64(len(combinedData))

	if session.ExpectedSha256 != "" && session.ExpectedSha256 != expectedSha256 {
		return nil, fmt.Errorf("%w: re-assembled parts sha256 %s differs from recorded expected_sha256 %s",
			ErrStorageIntegrityMismatch, expectedSha256, session.ExpectedSha256)
	}

	// TX 2: Durably checkpoint expected_sha256 before writing final blob, fenced by completion_token.
	err = sm.app.RunInTransactionWithContext(ctx, func(txApp App) error {
		var existingUploadId string
		existErr := txApp.DB().NewQuery(`
			SELECT [[upload_id]]
			FROM {{_storage_objects}}
			WHERE [[bucket_id]] = {:bucketId} AND [[name]] = {:name}
			LIMIT 1
		`).Bind(dbx.Params{
			"bucketId": session.BucketId,
			"name":     session.Name,
		}).Row(&existingUploadId)
		if existErr == nil && existingUploadId != session.Id {
			return fmt.Errorf("%w: unrelated object created at %s/%s",
				ErrStorageObjectConflict, session.BucketId, session.Name)
		}
		if existErr != nil && !errors.Is(existErr, sql.ErrNoRows) {
			return existErr
		}

		res, upErr := txApp.DB().NewQuery(`
			UPDATE {{_storage_uploads}}
			SET [[expected_sha256]] = {:sha},
			    [[total_size]] = {:size},
			    [[updated]] = now()
			WHERE [[id]] = {:id}
			  AND [[status]] = 'completing'
			  AND [[completion_token]] = {:token}
		`).Bind(dbx.Params{
			"id":    uploadId,
			"token": completionToken,
			"sha":   expectedSha256,
			"size":  expectedSize,
		}).Execute()
		if upErr != nil {
			return upErr
		}
		if aff, _ := res.RowsAffected(); aff == 0 {
			return fmt.Errorf("upload session %s completion lease lost before blob write", uploadId)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrStorageObjectConflict) {
			sm.revertCompletingToPending(uploadId, completionToken)
		}
		return nil, err
	}

	fileKey := sm.objectKey(session.BucketId, session.Name)

	if hooks := sm.getFaultHooks(); hooks != nil && hooks.beforeFinalBlobWrite != nil {
		if hookErr := hooks.beforeFinalBlobWrite(uploadId, fileKey); hookErr != nil {
			sm.revertCompletingToPending(uploadId, completionToken)
			return nil, hookErr
		}
	}

	// Write final assembled blob to storage and immediately verify stored bytes
	if err := fsys.Upload(combinedData, fileKey); err != nil {
		sm.revertCompletingToPending(uploadId, completionToken)
		return nil, fmt.Errorf("failed to write assembled object to storage: %w", err)
	}
	if err := sm.verifyStoredBlobIntegrity(fsys, fileKey, expectedSize, expectedSha256); err != nil {
		sm.revertCompletingToPending(uploadId, completionToken)
		return nil, err
	}

	if hooks := sm.getFaultHooks(); hooks != nil && hooks.afterFinalBlobWrite != nil {
		if hookErr := hooks.afterFinalBlobWrite(uploadId, fileKey); hookErr != nil {
			// Do not revert to pending here: simulates crash after blob write before metadata commit.
			return nil, hookErr
		}
	}

	// TX 3: Fenced _storage_objects metadata commit bound to upload_id and completion_token.
	var obj *StorageObject
	err = sm.app.RunInTransactionWithContext(ctx, func(txApp App) error {
		var curStatus, curToken string
		lockErr := txApp.DB().NewQuery(`
			SELECT [[status]], [[completion_token]]
			FROM {{_storage_uploads}}
			WHERE [[id]] = {:id}
			FOR UPDATE
		`).Bind(dbx.Params{"id": uploadId}).Row(&curStatus, &curToken)
		if lockErr != nil {
			return lockErr
		}
		if curStatus != "completing" || curToken != completionToken {
			return fmt.Errorf("upload session %s completion lease lost before metadata commit", uploadId)
		}

		// Verify no unrelated object exists at (bucket_id, name)
		var existingUploadId string
		existErr := txApp.DB().NewQuery(`
			SELECT [[upload_id]]
			FROM {{_storage_objects}}
			WHERE [[bucket_id]] = {:bucketId} AND [[name]] = {:name}
			FOR UPDATE
		`).Bind(dbx.Params{
			"bucketId": session.BucketId,
			"name":     session.Name,
		}).Row(&existingUploadId)
		if existErr == nil && existingUploadId != session.Id {
			return fmt.Errorf("%w: unrelated object exists at %s/%s",
				ErrStorageObjectConflict, session.BucketId, session.Name)
		}
		if existErr != nil && !errors.Is(existErr, sql.ErrNoRows) {
			return existErr
		}

		objId := security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
		now := types.NowDateTime()
		committed := &StorageObject{}
		insErr := txApp.DB().NewQuery(`
			INSERT INTO {{_storage_objects}} (
				[[id]], [[bucket_id]], [[name]], [[owner_id]], [[size]], [[mime_type]],
				[[etag]], [[metadata]], [[upload_id]], [[completion_token]], [[created]], [[updated]]
			) VALUES (
				{:id}, {:bucketId}, {:name}, {:ownerId}, {:size}, {:mimeType},
				{:etag}, 'null'::jsonb, {:uploadId}, {:completionToken}, {:created}, {:updated}
			)
			ON CONFLICT ([[bucket_id]], [[name]]) DO UPDATE SET
				[[owner_id]] = EXCLUDED.[[owner_id]],
				[[size]] = EXCLUDED.[[size]],
				[[mime_type]] = EXCLUDED.[[mime_type]],
				[[etag]] = EXCLUDED.[[etag]],
				[[completion_token]] = EXCLUDED.[[completion_token]],
				[[updated]] = now()
			WHERE {{_storage_objects}}.[[upload_id]] = {:uploadId}
			RETURNING [[id]], [[bucket_id]], [[name]], [[owner_id]], [[size]], [[mime_type]],
			          [[etag]], [[metadata]], [[upload_id]], [[completion_token]], [[created]], [[updated]]
		`).Bind(dbx.Params{
			"id":              objId,
			"bucketId":        session.BucketId,
			"name":            session.Name,
			"ownerId":         session.OwnerId,
			"size":            expectedSize,
			"mimeType":        "application/octet-stream",
			"etag":            expectedSha256,
			"uploadId":        session.Id,
			"completionToken": completionToken,
			"created":         now.String(),
			"updated":         now.String(),
		}).One(committed)
		if insErr != nil {
			if errors.Is(insErr, sql.ErrNoRows) {
				return fmt.Errorf("%w: unrelated object exists at %s/%s",
					ErrStorageObjectConflict, session.BucketId, session.Name)
			}
			return fmt.Errorf("failed to commit storage object metadata: %w", insErr)
		}

		// Record completed_object_id durably on the session before acknowledging completion
		_, upErr := txApp.DB().NewQuery(`
			UPDATE {{_storage_uploads}}
			SET [[completed_object_id]] = {:objId},
			    [[updated]] = now()
			WHERE [[id]] = {:id}
			  AND [[status]] = 'completing'
			  AND [[completion_token]] = {:token}
		`).Bind(dbx.Params{
			"id":    uploadId,
			"token": completionToken,
			"objId": committed.Id,
		}).Execute()
		if upErr != nil {
			return upErr
		}

		obj = committed
		return nil
	})
	if err != nil {
		return nil, err
	}

	if hooks := sm.getFaultHooks(); hooks != nil && hooks.afterMetadataCommit != nil {
		if hookErr := hooks.afterMetadataCommit(uploadId, obj); hookErr != nil {
			// Simulates crash after metadata commit but before status acknowledgment.
			return nil, hookErr
		}
	}

	// TX 4: Fenced transition to 'completed' and durable enqueue of temporary part keys for cleanup.
	if err := sm.finalizeCompletedSessionTx(ctx, &session, completionToken, obj.Id, expectedSha256); err != nil {
		sm.app.Logger().Error("failed to record upload session completed status", "id", uploadId, "error", err)
		return nil, fmt.Errorf("failed to finalize upload status to completed: %w", err)
	}

	// Clean up parts only after durable completion commit
	sm.cleanupSessionPartsBestEffort(fsys, &session)

	return obj, nil
}

func (sm *StorageManager) getBucketTx(txApp App, bucketId string) (*StorageBucket, error) {
	b := &StorageBucket{}
	err := txApp.DB().NewQuery(`
		SELECT [[id]], [[name]], [[public]], [[file_size_limit]], [[allowed_mime_types]], [[created]], [[updated]]
		FROM {{_storage_buckets}}
		WHERE [[id]] = {:val} OR [[name]] = {:val}
		LIMIT 1
	`).Bind(dbx.Params{"val": bucketId}).One(b)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStorageBucketNotFound
		}
		return nil, err
	}
	return b, nil
}

func validateContiguousParts(session *StorageUploadSession, bucket *StorageBucket) error {
	if len(session.Parts) == 0 {
		return fmt.Errorf("cannot complete multipart upload with zero parts")
	}

	slices.SortFunc(session.Parts, func(a, b StorageUploadPart) int {
		return cmp.Compare(a.PartNumber, b.PartNumber)
	})

	var totalSize int64
	for idx, p := range session.Parts {
		expectedPartNum := idx + 1
		if p.PartNumber != expectedPartNum {
			return fmt.Errorf("multipart upload has missing or non-contiguous parts: expected part %d, got %d", expectedPartNum, p.PartNumber)
		}
		totalSize += p.Size
	}
	session.TotalSize = totalSize

	if bucket != nil && bucket.FileSizeLimit > 0 && totalSize > bucket.FileSizeLimit {
		return ErrStorageFileSizeExceeded
	}
	return nil
}

func (sm *StorageManager) finalizeCompletedSessionTx(
	ctx context.Context,
	session *StorageUploadSession,
	completionToken string,
	objectId string,
	expectedSha256 string,
) error {
	return sm.app.RunInTransactionWithContext(ctx, func(txApp App) error {
		res, err := txApp.DB().NewQuery(`
			UPDATE {{_storage_uploads}}
			SET [[status]] = 'completed',
			    [[completed_object_id]] = {:objId},
			    [[expected_sha256]] = {:sha},
			    [[completion_leased_until]] = NULL,
			    [[updated]] = now()
			WHERE [[id]] = {:id}
			  AND [[status]] = 'completing'
			  AND [[completion_token]] = {:token}
		`).Bind(dbx.Params{
			"id":    session.Id,
			"token": completionToken,
			"objId": objectId,
			"sha":   expectedSha256,
		}).Execute()
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return fmt.Errorf("upload session %s was not in completing state with matching token during final status update", session.Id)
		}

		for _, part := range session.Parts {
			k := part.StorageKey
			if k == "" {
				k = sm.uploadPartKey(session.Id, part.PartNumber)
			}
			if qErr := sm.enqueueCleanupKeyTx(txApp, session.Id, k, "completed"); qErr != nil {
				return qErr
			}
		}
		return nil
	})
}

func (sm *StorageManager) cleanupSessionPartsBestEffort(fsys *filesystem.System, session *StorageUploadSession) {
	for _, part := range session.Parts {
		k := part.StorageKey
		if k != "" {
			_ = sm.deleteFileAndDequeueCleanup(fsys, k)
		}
		legacyKey := sm.uploadPartKey(session.Id, part.PartNumber)
		if legacyKey != k {
			_ = fsys.Delete(legacyKey)
		}
	}
}

// AbortMultipartUpload cancels an active pending session and purges all staged parts.
// It safely rejects completed, expired, or in-flight completing sessions.
func (sm *StorageManager) AbortMultipartUpload(uploadId string) error {
	var session StorageUploadSession

	err := sm.app.RunInTransaction(func(txApp App) error {
		err := txApp.DB().NewQuery(`
			SELECT [[id]], [[bucket_id]], [[name]], [[parts]], [[status]]
			FROM {{_storage_uploads}}
			WHERE [[id]] = {:id}
			FOR UPDATE
		`).Bind(dbx.Params{"id": uploadId}).One(&session)
		if err != nil {
			return fmt.Errorf("upload session not found: %w", err)
		}
		if session.Status == "expired" {
			return fmt.Errorf("%w: cannot abort expired upload session %s", ErrStorageUploadExpired, uploadId)
		}
		if session.Status == "completed" || session.Status == "completing" {
			return fmt.Errorf("cannot abort upload in %s state", session.Status)
		}

		// Durably journal all part keys before deleting the session row so crashes cannot leak chunks.
		for _, part := range session.Parts {
			k := part.StorageKey
			if k == "" {
				k = sm.uploadPartKey(uploadId, part.PartNumber)
			}
			if qErr := sm.enqueueCleanupKeyTx(txApp, uploadId, k, "aborted"); qErr != nil {
				return qErr
			}
		}

		res, err := txApp.DB().NewQuery(`
			DELETE FROM {{_storage_uploads}} WHERE [[id]] = {:id} AND [[status]] = 'pending'
		`).Bind(dbx.Params{"id": uploadId}).Execute()
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return fmt.Errorf("upload session %s cannot be aborted (not pending)", uploadId)
		}
		return nil
	})

	if err != nil {
		return err
	}

	fsys, err := sm.app.NewFilesystem()
	if err == nil {
		defer fsys.Close()
		sm.cleanupSessionPartsBestEffort(fsys, &session)
	}

	return nil
}

// CleanupOrphanedUploads executes a safe two-stage garbage collection lifecycle for abandoned
// multipart uploads without holding PostgreSQL transactions open during slow object-store I/O:
//
//   - Stage A (Database-coordinated expiration): Atomically locks stale pending sessions
//     (updated older than maxAge) with FOR UPDATE SKIP LOCKED, journals their part keys into
//     _storage_upload_cleanup, and transitions them to status='expired' with parts='[]' so
//     expired sessions can never be resumed or retain dangling references to deleted chunks.
//   - Stage B (Idempotent object cleanup): Deletes journaled keys and legacy/untracked orphan
//     files whose sessions are terminal or absent, strictly preserving active pending and
//     completing sessions. Filesystem deletion failures are recorded and returned rather than
//     counted as successful cleanups.
func (sm *StorageManager) CleanupOrphanedUploads(ctx context.Context, maxAge time.Duration) (int, error) {
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	maxAgeMs := maxAge.Milliseconds()
	if maxAgeMs < 1 {
		maxAgeMs = 1
	}

	if hooks := sm.getFaultHooks(); hooks != nil && hooks.beforeGCStageA != nil {
		if err := hooks.beforeGCStageA(); err != nil {
			return 0, err
		}
	}

	// Stage A: Database-coordinated expiration (no object-store I/O inside transaction).
	var expiredIds []string
	err := sm.app.RunInTransactionWithContext(ctx, func(txApp App) error {
		var stale []StorageUploadSession
		qErr := txApp.DB().NewQuery(`
			SELECT [[id]], [[parts]]
			FROM {{_storage_uploads}}
			WHERE [[status]] = 'pending'
			  AND [[updated]] < now() - ({:maxAgeMs} * INTERVAL '1 millisecond')
			FOR UPDATE SKIP LOCKED
		`).Bind(dbx.Params{"maxAgeMs": maxAgeMs}).All(&stale)
		if qErr != nil {
			return qErr
		}

		for _, sess := range stale {
			for _, part := range sess.Parts {
				k := part.StorageKey
				if k == "" {
					k = sm.uploadPartKey(sess.Id, part.PartNumber)
				}
				if enqErr := sm.enqueueCleanupKeyTx(txApp, sess.Id, k, "expired"); enqErr != nil {
					return enqErr
				}
			}

			res, upErr := txApp.DB().NewQuery(`
				UPDATE {{_storage_uploads}}
				SET [[status]] = 'expired',
				    [[parts]] = '[]'::jsonb,
				    [[total_size]] = 0,
				    [[updated]] = now()
				WHERE [[id]] = {:id} AND [[status]] = 'pending'
			`).Bind(dbx.Params{"id": sess.Id}).Execute()
			if upErr != nil {
				return upErr
			}
			if aff, _ := res.RowsAffected(); aff > 0 {
				expiredIds = append(expiredIds, sess.Id)
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("multipart orphan GC stage A failed: %w", err)
	}

	if hooks := sm.getFaultHooks(); hooks != nil && hooks.afterGCStageA != nil {
		if hookErr := hooks.afterGCStageA(expiredIds); hookErr != nil {
			return 0, hookErr
		}
	}

	// Stage B: Idempotent object-store cleanup outside database transactions.
	fsys, err := sm.app.NewFilesystem()
	if err != nil {
		return 0, err
	}
	defer fsys.Close()

	purged := 0
	var cleanupErrs []error

	// Stage B.1: Drain durable cleanup journal (_storage_upload_cleanup).
	var tasks []struct {
		Id         string `db:"id"`
		UploadId   string `db:"upload_id"`
		StorageKey string `db:"storage_key"`
	}
	if err := sm.app.DB().NewQuery(`
		SELECT [[id]], [[upload_id]], [[storage_key]]
		FROM {{_storage_upload_cleanup}}
		ORDER BY [[created]] ASC
	`).WithContext(ctx).All(&tasks); err != nil {
		return 0, fmt.Errorf("failed to query cleanup journal: %w", err)
	}

	for _, task := range tasks {
		// Defense-in-depth: never delete a key still referenced by an active pending/completing session.
		referenced, refErr := sm.isKeyReferencedByActiveSession(ctx, task.UploadId, task.StorageKey)
		if refErr != nil {
			cleanupErrs = append(cleanupErrs, refErr)
			continue
		}
		if referenced {
			continue
		}

		if hooks := sm.getFaultHooks(); hooks != nil && hooks.beforeGCDeleteKey != nil {
			if hookErr := hooks.beforeGCDeleteKey(task.StorageKey); hookErr != nil {
				_, _ = sm.app.DB().NewQuery(`
					UPDATE {{_storage_upload_cleanup}}
					SET [[attempts]] = [[attempts]] + 1, [[last_error]] = {:err}, [[updated]] = now()
					WHERE [[id]] = {:id}
				`).Bind(dbx.Params{"id": task.Id, "err": hookErr.Error()}).Execute()
				cleanupErrs = append(cleanupErrs, hookErr)
				continue
			}
		}

		delErr := fsys.Delete(task.StorageKey)
		if delErr != nil && !errors.Is(delErr, filesystem.ErrNotFound) {
			_, _ = sm.app.DB().NewQuery(`
				UPDATE {{_storage_upload_cleanup}}
				SET [[attempts]] = [[attempts]] + 1, [[last_error]] = {:err}, [[updated]] = now()
				WHERE [[id]] = {:id}
			`).Bind(dbx.Params{"id": task.Id, "err": delErr.Error()}).Execute()
			cleanupErrs = append(cleanupErrs, fmt.Errorf("failed to delete %s: %w", task.StorageKey, delErr))
			continue
		}

		res, dqErr := sm.app.DB().NewQuery(`
			DELETE FROM {{_storage_upload_cleanup}} WHERE [[id]] = {:id}
		`).WithContext(ctx).Bind(dbx.Params{"id": task.Id}).Execute()
		if dqErr != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("failed to dequeue cleanup task %s: %w", task.Id, dqErr))
			continue
		}
		if aff, _ := res.RowsAffected(); aff > 0 && delErr == nil {
			purged++
		}
	}

	// Stage B.2: Scan _storage_uploads prefix for legacy or untracked orphan objects.
	items, err := fsys.List("_storage_uploads")
	if err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("failed to list _storage_uploads: %w", err))
		return purged, errors.Join(cleanupErrs...)
	}

	for _, item := range items {
		if item.IsDir {
			continue
		}
		// Key format: _storage_uploads/<uploadId>/part_<num>[_<token>]
		trimmed := strings.TrimPrefix(item.Key, "_storage_uploads/")
		segments := strings.Split(trimmed, "/")
		if len(segments) < 2 || segments[0] == "" {
			continue
		}
		uploadId := segments[0]

		var session StorageUploadSession
		qErr := sm.app.DB().NewQuery(`
			SELECT [[id]], [[status]], [[parts]], [[updated]]
			FROM {{_storage_uploads}}
			WHERE [[id]] = {:id}
		`).WithContext(ctx).Bind(dbx.Params{"id": uploadId}).One(&session)

		shouldDelete := false
		if errors.Is(qErr, sql.ErrNoRows) {
			// Session row was aborted/deleted or never existed -> safe to delete orphan file.
			shouldDelete = true
		} else if qErr != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("failed to inspect session %s during GC: %w", uploadId, qErr))
			continue
		} else {
			switch session.Status {
			case "completed", "expired", "aborted":
				shouldDelete = true
			case "pending", "completing":
				// Never delete any key referenced by an active session.
				if sm.sessionReferencesKey(&session, item.Key) {
					shouldDelete = false
				} else if time.Since(item.ModTime) > maxAge {
					// Unreferenced stale temporary artifact (e.g. from a crashed UploadPart before DB commit).
					shouldDelete = true
				}
			}
		}

		if !shouldDelete {
			continue
		}

		if hooks := sm.getFaultHooks(); hooks != nil && hooks.beforeGCDeleteKey != nil {
			if hookErr := hooks.beforeGCDeleteKey(item.Key); hookErr != nil {
				cleanupErrs = append(cleanupErrs, hookErr)
				continue
			}
		}

		delErr := fsys.Delete(item.Key)
		if delErr != nil && !errors.Is(delErr, filesystem.ErrNotFound) {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("failed to delete orphan %s: %w", item.Key, delErr))
			continue
		}
		if delErr == nil {
			purged++
		}
	}

	return purged, errors.Join(cleanupErrs...)
}

func (sm *StorageManager) sessionReferencesKey(session *StorageUploadSession, key string) bool {
	for _, p := range session.Parts {
		if p.StorageKey == key {
			return true
		}
		if p.StorageKey == "" && sm.uploadPartKey(session.Id, p.PartNumber) == key {
			return true
		}
	}
	return false
}

func (sm *StorageManager) isKeyReferencedByActiveSession(ctx context.Context, uploadId, key string) (bool, error) {
	var session StorageUploadSession
	err := sm.app.DB().NewQuery(`
		SELECT [[id]], [[status]], [[parts]]
		FROM {{_storage_uploads}}
		WHERE [[id]] = {:id}
	`).WithContext(ctx).Bind(dbx.Params{"id": uploadId}).One(&session)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if session.Status != "pending" && session.Status != "completing" {
		return false, nil
	}
	return sm.sessionReferencesKey(&session, key), nil
}
