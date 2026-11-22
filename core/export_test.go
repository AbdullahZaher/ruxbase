package core

// SetTestDatabaseURLFunc sets the tests only hook that provides a new
// database for the apps created without a connection string.
func SetTestDatabaseURLFunc(f func() string) {
	testDatabaseURLFunc = f
}

// NormalizeViewQueryId exports normalizeViewQueryId for the tests.
func NormalizeViewQueryId(app App, query string) (string, error) {
	return normalizeViewQueryId(app, query)
}

// FindSchemaViewNames returns the names of the findSchemaViews result for the tests.
func FindSchemaViewNames(app App) ([]string, error) {
	views, err := findSchemaViews(app)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(views))
	for i, v := range views {
		names[i] = v.Name
	}

	return names, nil
}

// StorageFaultHooksForTest exposes deterministic fault-injection hooks for package core_test.
type StorageFaultHooksForTest struct {
	BeforePartCommit     func(uploadId string, partNumber int, partKey string) error
	AfterCompletionClaim func(uploadId string, completionToken string) error
	BeforeFinalBlobWrite func(uploadId string, fileKey string) error
	AfterFinalBlobWrite  func(uploadId string, fileKey string) error
	AfterMetadataCommit  func(uploadId string, obj *StorageObject) error
	BeforeGCStageA       func() error
	AfterGCStageA        func(expiredUploadIds []string) error
	BeforeGCDeleteKey    func(storageKey string) error
}

// SetStorageFaultHooksForTest configures test-only fault hooks on a StorageManager.
func SetStorageFaultHooksForTest(sm *StorageManager, h *StorageFaultHooksForTest) {
	sm.hooksMu.Lock()
	defer sm.hooksMu.Unlock()
	if h == nil {
		sm.faultHooks = nil
		return
	}
	sm.faultHooks = &storageFaultHooks{
		beforePartCommit:     h.BeforePartCommit,
		afterCompletionClaim: h.AfterCompletionClaim,
		beforeFinalBlobWrite: h.BeforeFinalBlobWrite,
		afterFinalBlobWrite:  h.AfterFinalBlobWrite,
		afterMetadataCommit:  h.AfterMetadataCommit,
		beforeGCStageA:       h.BeforeGCStageA,
		afterGCStageA:        h.AfterGCStageA,
		beforeGCDeleteKey:    h.BeforeGCDeleteKey,
	}
}

// OutboxFaultHooksForTest exposes deterministic fault-injection hooks for package core_test.
type OutboxFaultHooksForTest struct {
	BeforeClaim    func() error
	AfterClaim     func(events []*OutboxEvent) error
	BeforeRenew    func(evt *OutboxEvent) error
	BeforeMarkPub  func(evt *OutboxEvent) error
	AfterMarkPub   func(evt *OutboxEvent, affected int64) error
	BeforeMarkFail func(evt *OutboxEvent) error
}

// SetOutboxFaultHooksForTest configures test-only fault hooks on a JobsManager.
func SetOutboxFaultHooksForTest(jm *JobsManager, h *OutboxFaultHooksForTest) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	if h == nil {
		jm.faultHooks = nil
		return
	}
	jm.faultHooks = &outboxFaultHooks{
		beforeClaim:    h.BeforeClaim,
		afterClaim:     h.AfterClaim,
		beforeRenew:    h.BeforeRenew,
		beforeMarkPub:  h.BeforeMarkPub,
		afterMarkPub:   h.AfterMarkPub,
		beforeMarkFail: h.BeforeMarkFail,
	}
}
