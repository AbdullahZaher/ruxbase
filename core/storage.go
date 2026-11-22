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
	"time"

	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/AbdullahZaher/ruxbase/tools/types"
	"github.com/pocketbase/dbx"
)

var (
	ErrStorageBucketNotFound     = errors.New("storage bucket not found")
	ErrStorageObjectNotFound     = errors.New("storage object not found")
	ErrStorageAccessDenied       = errors.New("storage access denied: cross-tenant or private object")
	ErrStorageFileSizeExceeded   = errors.New("storage error: file size exceeds bucket limit")
	ErrStorageInvalidMimeType    = errors.New("storage error: MIME type not allowed in bucket")
	ErrStorageInvalidSignedToken = errors.New("storage error: invalid or expired signed URL token")
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
	Id       string             `db:"id" json:"id"`
	BucketId string             `db:"bucket_id" json:"bucketId"`
	Name     string             `db:"name" json:"name"`
	OwnerId  string             `db:"owner_id" json:"ownerId"`
	Size     int64              `db:"size" json:"size"`
	MimeType string             `db:"mime_type" json:"mimeType"`
	Etag     string             `db:"etag" json:"etag"`
	Metadata types.JSONMap[any] `db:"metadata" json:"metadata,omitempty"`
	Created  types.DateTime     `db:"created" json:"created"`
	Updated  types.DateTime     `db:"updated" json:"updated"`
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
	Id        string                             `db:"id" json:"id"`
	BucketId  string                             `db:"bucket_id" json:"bucketId"`
	Name      string                             `db:"name" json:"name"`
	OwnerId   string                             `db:"owner_id" json:"ownerId"`
	Parts     types.JSONArray[StorageUploadPart] `db:"parts" json:"parts"`
	TotalSize int64                              `db:"total_size" json:"totalSize"`
	Status    string                             `db:"status" json:"status"`
	Created   types.DateTime                     `db:"created" json:"created"`
	Updated   types.DateTime                     `db:"updated" json:"updated"`
}

// StorageManager manages storage buckets, objects, and permissions.
type StorageManager struct {
	app App
}

// NewStorageManager creates a new StorageManager instance.
func NewStorageManager(app App) *StorageManager {
	return &StorageManager{app: app}
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

	// 3. Compute ETag (SHA-256)
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
			[[etag]], [[metadata]], [[created]], [[updated]]
		) VALUES (
			{:id}, {:bucketId}, {:name}, {:ownerId}, {:size}, {:mimeType},
			{:etag}, {:metadata}::jsonb, {:created}, {:updated}
		)
		ON CONFLICT ([[bucket_id]], [[name]]) DO UPDATE SET
			[[owner_id]] = EXCLUDED.[[owner_id]],
			[[size]] = EXCLUDED.[[size]],
			[[mime_type]] = EXCLUDED.[[mime_type]],
			[[etag]] = EXCLUDED.[[etag]],
			[[metadata]] = EXCLUDED.[[metadata]],
			[[updated]] = now()
		RETURNING [[id]], [[bucket_id]], [[name]], [[owner_id]], [[size]], [[mime_type]],
		          [[etag]], [[metadata]], [[created]], [[updated]]
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
		       [[etag]], [[metadata]], [[created]], [[updated]]
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
		return nil
	})

	if err != nil {
		_ = fsys.Delete(partKey)
		return nil, err
	}

	// Safely clean up the replaced older attempt's temporary file if any
	if oldKeyToDelete != "" && oldKeyToDelete != partKey {
		_ = fsys.Delete(oldKeyToDelete)
	}

	return &part, nil
}

// CompleteMultipartUpload combines all uploaded parts into the final storage object.
// It verifies contiguous parts, validates SHA-256 ETags, enforces an atomic state machine,
// and supports crash recovery if object creation succeeded before final acknowledgment.
func (sm *StorageManager) CompleteMultipartUpload(ctx context.Context, uploadId string) (*StorageObject, error) {
	var session StorageUploadSession
	var alreadyCompletedObj *StorageObject

	// Atomically verify pending status, non-empty contiguous parts, and transition to 'completing'
	err := sm.app.RunInTransactionWithContext(ctx, func(txApp App) error {
		err := txApp.DB().NewQuery(`
			SELECT [[id]], [[bucket_id]], [[name]], [[owner_id]], [[parts]], [[total_size]], [[status]], [[created]], [[updated]]
			FROM {{_storage_uploads}}
			WHERE [[id]] = {:id}
			FOR UPDATE
		`).Bind(dbx.Params{"id": uploadId}).One(&session)
		if err != nil {
			return fmt.Errorf("upload session not found: %w", err)
		}

		// Reject already completed upload sessions (cannot complete twice)
		if session.Status == "completed" {
			return fmt.Errorf("cannot complete already completed upload session")
		}

		// Crash recovery check: if session is in 'completing' state, check if object was already created
		if session.Status == "completing" {
			existing := &StorageObject{}
			objErr := txApp.DB().NewQuery(`
				SELECT [[id]], [[bucket_id]], [[name]], [[owner_id]], [[size]], [[mime_type]],
				       [[etag]], [[metadata]], [[created]], [[updated]]
				FROM {{_storage_objects}}
				WHERE [[bucket_id]] = {:bucketId} AND [[name]] = {:name}
			`).Bind(dbx.Params{
				"bucketId": session.BucketId,
				"name":     session.Name,
			}).One(existing)
			if objErr == nil && existing.Size == session.TotalSize {
				// Object was persisted before worker crashed; finalize session status to completed
				res, finalizeErr := txApp.DB().NewQuery(`
					UPDATE {{_storage_uploads}}
					SET [[status]] = 'completed',
					    [[updated]] = now()
					WHERE [[id]] = {:id}
				`).Bind(dbx.Params{"id": uploadId}).Execute()
				if finalizeErr == nil {
					if aff, _ := res.RowsAffected(); aff > 0 {
						alreadyCompletedObj = existing
						return nil
					}
				}
			}
			return fmt.Errorf("upload session is currently being assembled (status: completing)")
		}

		if session.Status != "pending" {
			return fmt.Errorf("upload session not pending (status: %s)", session.Status)
		}
		if len(session.Parts) == 0 {
			return fmt.Errorf("cannot complete multipart upload with zero parts")
		}

		// Sort parts by PartNumber
		slices.SortFunc(session.Parts, func(a, b StorageUploadPart) int {
			return cmp.Compare(a.PartNumber, b.PartNumber)
		})

		// Verify contiguous part numbers starting from 1
		for idx, p := range session.Parts {
			expectedPartNum := idx + 1
			if p.PartNumber != expectedPartNum {
				return fmt.Errorf("multipart upload has missing or non-contiguous parts: expected part %d, got %d", expectedPartNum, p.PartNumber)
			}
		}

		// Atomically lock session into 'completing'
		res, err := txApp.DB().NewQuery(`
			UPDATE {{_storage_uploads}}
			SET [[status]] = 'completing',
			    [[updated]] = now()
			WHERE [[id]] = {:id} AND [[status]] = 'pending'
		`).Bind(dbx.Params{"id": uploadId}).Execute()
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			return fmt.Errorf("failed to transition upload session %s to completing", uploadId)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	if alreadyCompletedObj != nil {
		return alreadyCompletedObj, nil
	}

	fsys, err := sm.app.NewFilesystem()
	if err != nil {
		// Revert to pending
		_, _ = sm.app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[status]] = 'pending', [[updated]] = now() WHERE [[id]] = {:id}").Bind(dbx.Params{"id": uploadId}).Execute()
		return nil, err
	}
	defer fsys.Close()

	// Read and concatenate all parts in verified order
	var combinedData []byte
	if session.TotalSize > 0 && session.TotalSize < 2*1024*1024*1024 {
		combinedData = make([]byte, 0, session.TotalSize)
	}

	for _, part := range session.Parts {
		partKey := part.StorageKey
		if partKey == "" {
			partKey = sm.uploadPartKey(uploadId, part.PartNumber)
		}
		reader, err := fsys.GetReader(partKey)
		if err != nil {
			_, _ = sm.app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[status]] = 'pending', [[updated]] = now() WHERE [[id]] = {:id}").Bind(dbx.Params{"id": uploadId}).Execute()
			return nil, fmt.Errorf("missing upload part %d: %w", part.PartNumber, err)
		}
		partBytes, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			_, _ = sm.app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[status]] = 'pending', [[updated]] = now() WHERE [[id]] = {:id}").Bind(dbx.Params{"id": uploadId}).Execute()
			return nil, fmt.Errorf("failed to read part %d: %w", part.PartNumber, readErr)
		}

		// Verify ETag checksum of part
		h := sha256.Sum256(partBytes)
		etag := hex.EncodeToString(h[:])
		if etag != part.Etag {
			_, _ = sm.app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[status]] = 'pending', [[updated]] = now() WHERE [[id]] = {:id}").Bind(dbx.Params{"id": uploadId}).Execute()
			return nil, fmt.Errorf("part %d checksum mismatch: expected %s, got %s", part.PartNumber, part.Etag, etag)
		}

		combinedData = append(combinedData, partBytes...)
	}

	// Save final assembled object via UploadObject
	obj, err := sm.UploadObject(ctx, session.BucketId, session.Name, session.OwnerId, "application/octet-stream", combinedData, nil)
	if err != nil {
		_, _ = sm.app.DB().NewQuery("UPDATE {{_storage_uploads}} SET [[status]] = 'pending', [[updated]] = now() WHERE [[id]] = {:id}").Bind(dbx.Params{"id": uploadId}).Execute()
		return nil, err
	}

	// Mark session completed in DB with affected-row and error check
	res, updateErr := sm.app.DB().NewQuery(`
		UPDATE {{_storage_uploads}}
		SET [[status]] = 'completed',
		    [[updated]] = now()
		WHERE [[id]] = {:id} AND [[status]] = 'completing'
	`).Bind(dbx.Params{"id": uploadId}).Execute()
	if updateErr != nil {
		sm.app.Logger().Error("failed to record upload session completed status", "id", uploadId, "error", updateErr)
		return nil, fmt.Errorf("failed to finalize upload status to completed: %w", updateErr)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, fmt.Errorf("upload session %s was not in completing state during final status update", uploadId)
	}

	// Clean up parts only after successful assembly and metadata commit
	for _, part := range session.Parts {
		k := part.StorageKey
		if k != "" {
			_ = fsys.Delete(k)
		}
		_ = fsys.Delete(sm.uploadPartKey(uploadId, part.PartNumber))
	}

	return obj, nil
}

// AbortMultipartUpload cancels an active session and purges all staged parts.
// It safely rejects aborted completed or in-flight completing sessions.
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
		if session.Status == "completed" || session.Status == "completing" {
			return fmt.Errorf("cannot abort upload in %s state", session.Status)
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
		for _, part := range session.Parts {
			if part.StorageKey != "" {
				_ = fsys.Delete(part.StorageKey)
			}
			_ = fsys.Delete(sm.uploadPartKey(uploadId, part.PartNumber))
		}
	}

	return nil
}

// CleanupOrphanedUploads purges temporary files of upload sessions that are either
// completed, aborted, or expired (older than maxAge without completion).
// Active pending sessions within maxAge are strictly preserved.
func (sm *StorageManager) CleanupOrphanedUploads(ctx context.Context, maxAge time.Duration) (int, error) {
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}

	fsys, err := sm.app.NewFilesystem()
	if err != nil {
		return 0, err
	}
	defer fsys.Close()

	items, err := fsys.List("_storage_uploads")
	if err != nil {
		return 0, err
	}

	purged := 0
	for _, item := range items {
		// Key format: _storage_uploads/<uploadId>/part_<num>_<token>
		parts := strings.Split(strings.TrimPrefix(item.Key, "_storage_uploads/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		uploadId := parts[0]

		var session StorageUploadSession
		err := sm.app.DB().NewQuery(`
			SELECT [[id]], [[status]], [[updated]]
			FROM {{_storage_uploads}}
			WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": uploadId}).One(&session)

		shouldDelete := false
		if errors.Is(err, sql.ErrNoRows) {
			// Session no longer exists in DB -> orphaned
			shouldDelete = true
		} else if err == nil {
			if session.Status == "completed" {
				shouldDelete = true
			} else if session.Status == "pending" && time.Since(session.Updated.Time()) > maxAge {
				shouldDelete = true
			}
		}

		if shouldDelete {
			_ = fsys.Delete(item.Key)
			purged++
		}
	}

	return purged, nil
}
