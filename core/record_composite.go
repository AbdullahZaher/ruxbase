package core

import (
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
)

// FindRecordByCompositeKey searches for a single record in the specified collection
// matching all provided composite key column-value pairs.
func (app *BaseApp) FindRecordByCompositeKey(collectionNameOrId string, keys map[string]any) (*Record, error) {
	if len(keys) == 0 {
		return nil, errors.New("keys map cannot be empty")
	}

	collection, err := app.FindCollectionByNameOrId(collectionNameOrId)
	if err != nil {
		return nil, err
	}

	hashExp := dbx.HashExp{}
	for k, v := range keys {
		// Validate column identifier
		if !fieldNameRegex.MatchString(k) {
			return nil, fmt.Errorf("invalid column identifier in composite key: %q", k)
		}
		hashExp[collection.Name+"."+k] = v
	}

	record := &Record{}
	err = app.RecordQuery(collection).
		AndWhere(hashExp).
		Limit(1).
		One(record)
	if err != nil {
		return nil, err
	}

	return record, nil
}
