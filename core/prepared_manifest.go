package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/AbdullahZaher/ruxbase/internal/pgrolecheck"
)

// The two manifests are intentionally explicit even when the pools share a
// database. A main database success must not mask an auxiliary mismatch.
type preparedManifest struct {
	Main      pgrolecheck.GrantManifest `json:"main"`
	Auxiliary pgrolecheck.GrantManifest `json:"auxiliary"`
}

func parsePreparedManifest(raw, role string) (preparedManifest, error) {
	var manifest preparedManifest
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return preparedManifest{}, errors.New("invalid prepared grant manifest")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return preparedManifest{}, errors.New("unexpected trailing prepared manifest data")
	}
	if role == "" || manifest.Main.Role != role || manifest.Auxiliary.Role != role ||
		len(manifest.Main.Schemas) == 0 || len(manifest.Auxiliary.Schemas) == 0 {
		return preparedManifest{}, errors.New("both prepared manifest identities and schema contracts are required")
	}
	return manifest, nil
}

func checkPreparedManifest(ctx context.Context, main, aux *sql.DB, manifest preparedManifest) error {
	if _, err := manifest.Main.Check(ctx, main); err != nil {
		return errors.New("prepared main database grant manifest rejected")
	}
	if _, err := manifest.Auxiliary.Check(ctx, aux); err != nil {
		return errors.New("prepared auxiliary database grant manifest rejected")
	}
	return nil
}
