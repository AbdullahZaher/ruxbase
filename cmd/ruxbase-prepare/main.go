package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/AbdullahZaher/ruxbase/internal/operatorprep"
	"github.com/AbdullahZaher/ruxbase/internal/pgrolecheck"
)

// reviewedIdentities ensures the separated operator CLI cannot silently
// omit the declared provisioner or runtime identity. No credentials are read.
func reviewedIdentities(separate bool, getenv func(string) string) (*pgrolecheck.IdentityContract, error) {
	contract := pgrolecheck.IdentityContract{
		Runtime:      getenv("RUXBASE_PREPARE_RUNTIME_ROLE"),
		Migration:    getenv("RUXBASE_PREPARE_ROLE"),
		Provisioning: getenv("RUXBASE_PREPARE_PROVISIONING_ROLE"),
		Replication:  getenv("RUXBASE_PREPARE_REPLICATION_ROLE"),
	}
	if !separate {
		if contract.Runtime != "" || contract.Provisioning != "" || contract.Replication != "" {
			return nil, errors.New("declared separate identities require --preprovisioned-roles")
		}
		return nil, nil
	}
	if err := contract.Validate(); err != nil {
		return nil, err
	}
	return &contract, nil
}

func run(args []string) error {
	flags := flag.NewFlagSet("ruxbase-prepare", flag.ContinueOnError)
	apply := flags.Bool("apply-system", false, "confirm authorized offline system preparation")
	check := flags.Bool("check-system", false, "inspect system migration history without bootstrap")
	separate := flags.Bool("preprovisioned-roles", false, "require a separate preprovisioned migration principal")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *apply == *check {
		return errors.New("select exactly one of --apply-system or --check-system")
	}
	if *check && *separate {
		return errors.New("--preprovisioned-roles requires --apply-system")
	}
	identities, err := reviewedIdentities(*separate, os.Getenv)
	if err != nil {
		return err
	}
	if *check {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		status, err := operatorprep.CheckSystemHistory(ctx, os.Getenv("RUXBASE_PREPARE_DATABASE_URL"), os.Getenv("RUXBASE_PREPARE_LOGS_URL"), os.Getenv("RUXBASE_PREPARE_ROLE"))
		if writeErr := json.NewEncoder(os.Stdout).Encode(status); writeErr != nil {
			return writeErr
		}
		return err
	}

	err = operatorprep.Prepare(operatorprep.Config{
		DatabaseURL:            os.Getenv("RUXBASE_PREPARE_DATABASE_URL"),
		LogsDatabaseURL:        os.Getenv("RUXBASE_PREPARE_LOGS_URL"),
		ExpectedRole:           os.Getenv("RUXBASE_PREPARE_ROLE"),
		DataDir:                os.Getenv("RUXBASE_PREPARE_DATA_DIR"),
		EncryptionEnv:          os.Getenv("RUXBASE_PREPARE_ENCRYPTION_ENV"),
		PreprovisionedRLSRoles: *separate,
		IdentityContract:      identities,
	})
	if err == nil {
		fmt.Println("System preparation completed; serving and user migrations not started.")
	}
	return err
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
