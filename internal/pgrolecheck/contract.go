package pgrolecheck

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// IdentityContract declares separate database principals for the proposed
// serving and operator paths. It contains role names, never credentials.
// Replication is optional for deployments using application hook events.
type IdentityContract struct {
	Runtime      string `json:"runtime"`
	Migration    string `json:"migration"`
	Provisioning string `json:"provisioning"`
	Replication  string `json:"replication,omitempty"`
}

// Validate rejects ambiguous or shared identities before connection admission.
// Names are exact PostgreSQL identifiers, not case-folded aliases.
func (c IdentityContract) Validate() error {
	names := []string{c.Runtime, c.Migration, c.Provisioning}
	if c.Replication != "" {
		names = append(names, c.Replication)
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if name == "" || strings.TrimSpace(name) != name || strings.ContainsRune(name, 0) {
			return errors.New("invalid database identity in role contract")
		}
		if seen[name] {
			return errors.New("database identities must be distinct")
		}
		seen[name] = true
	}
	return nil
}

// AdmitRuntime checks the proposed runtime connection against the declared
// identity and the conservative authority screen. It does not certify object
// grants, bind later pool connections, or authorize production serving.
func (c IdentityContract) AdmitRuntime(ctx context.Context, db *sql.DB) (Report, error) {
	if err := c.Validate(); err != nil {
		return Report{}, err
	}
	report, err := Inspect(ctx, db)
	if err != nil {
		return Report{}, err
	}
	if report.SessionRole != c.Runtime || report.CurrentRole != c.Runtime {
		return report, errors.New("database identity does not match runtime contract")
	}
	if len(report.Findings()) != 0 {
		return report, errors.New("runtime identity failed authority screen")
	}
	return report, nil
}
