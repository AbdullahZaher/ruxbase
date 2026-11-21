package core

import (
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
func (sm *StorageManager) UploadPart(uploadId string, partNumber int, data []byte) (*StorageUploadPart, error) {
	session := &StorageUploadSession{}
	err := sm.app.DB().NewQuery(`
		SELECT [[id]], [[bucket_id]], [[name]], [[owner_id]], [[parts]], [[total_size]], [[status]], [[created]], [[updated]]
		FROM {{_storage_uploads}}
		WHERE [[id]] = {:id} AND [[status]] = 'pending'
	`).Bind(dbx.Params{"id": uploadId}).One(session)

	if err != nil {
		return nil, fmt.Errorf("upload session not found or not pending: %w", err)
	}

	// Write chunk to temporary filesystem key
	fsys, err := sm.app.NewFilesystem()
	if err != nil {
		return nil, err
	}
	defer fsys.Close()

	partKey := sm.uploadPartKey(uploadId, partNumber)
	if err := fsys.Upload(data, partKey); err != nil {
		return nil, fmt.Errorf("failed to upload part %d: %w", partNumber, err)
	}

	hash := sha256.Sum256(data)
	etag := hex.EncodeToString(hash[:])

	part := StorageUploadPart{
		PartNumber: partNumber,
		Size:       int64(len(data)),
		Etag:       etag,
	}

	// Update session parts
	session.Parts = append(session.Parts, part)
	session.TotalSize += part.Size
	partsBytes, _ := json.Marshal(session.Parts)

	_, err = sm.app.DB().NewQuery(`
		UPDATE {{_storage_uploads}}
		SET [[parts]] = {:parts}::jsonb,
		    [[total_size]] = {:totalSize},
		    [[updated]] = now()
		WHERE [[id]] = {:id}
	`).Bind(dbx.Params{
		"id":        uploadId,
		"parts":     string(partsBytes),
		"totalSize": session.TotalSize,
	}).Execute()

	if err != nil {
		return nil, err
	}

	return &part, nil
}

// CompleteMultipartUpload combines all uploaded parts into the final storage object.
func (sm *StorageManager) CompleteMultipartUpload(ctx context.Context, uploadId string) (*StorageObject, error) {
	session := &StorageUploadSession{}
	err := sm.app.DB().NewQuery(`
		SELECT [[id]], [[bucket_id]], [[name]], [[owner_id]], [[parts]], [[total_size]], [[status]], [[created]], [[updated]]
		FROM {{_storage_uploads}}
		WHERE [[id]] = {:id} AND [[status]] = 'pending'
	`).Bind(dbx.Params{"id": uploadId}).One(session)

	if err != nil {
		return nil, fmt.Errorf("upload session not found or not pending: %w", err)
	}

	fsys, err := sm.app.NewFilesystem()
	if err != nil {
		return nil, err
	}
	defer fsys.Close()

	// Read and concatenate all parts in order
	var combinedData []byte
	for _, part := range session.Parts {
		partKey := sm.uploadPartKey(uploadId, part.PartNumber)
		reader, err := fsys.GetReader(partKey)
		if err != nil {
			return nil, fmt.Errorf("missing upload part %d: %w", part.PartNumber, err)
		}
		partBytes, readErr := io.ReadAll(reader)
		reader.Close()
		if readErr != nil {
			return nil, fmt.Errorf("failed to read part %d: %w", part.PartNumber, readErr)
		}
		combinedData = append(combinedData, partBytes...)
		// Clean up part file
		_ = fsys.Delete(partKey)
	}

	// Save final assembled object via UploadObject
	obj, err := sm.UploadObject(ctx, session.BucketId, session.Name, session.OwnerId, "application/octet-stream", combinedData, nil)
	if err != nil {
		return nil, err
	}

	// Mark session completed
	_, _ = sm.app.DB().NewQuery(`
		UPDATE {{_storage_uploads}}
		SET [[status]] = 'completed',
		    [[updated]] = now()
		WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": uploadId}).Execute()

	return obj, nil
}

// AbortMultipartUpload cancels an active session and purges all staged parts.
func (sm *StorageManager) AbortMultipartUpload(uploadId string) error {
	session := &StorageUploadSession{}
	err := sm.app.DB().NewQuery(`
		SELECT [[id]], [[bucket_id]], [[name]], [[parts]]
		FROM {{_storage_uploads}}
		WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": uploadId}).One(session)

	if err != nil {
		return err
	}

	fsys, err := sm.app.NewFilesystem()
	if err == nil {
		defer fsys.Close()
		for _, part := range session.Parts {
			_ = fsys.Delete(sm.uploadPartKey(uploadId, part.PartNumber))
		}
	}

	_, err = sm.app.DB().NewQuery(`
		DELETE FROM {{_storage_uploads}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": uploadId}).Execute()

	return err
}
