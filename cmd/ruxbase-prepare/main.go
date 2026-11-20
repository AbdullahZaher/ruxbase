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
)

func run(args []string) error {
	flags := flag.NewFlagSet("ruxbase-prepare", flag.ContinueOnError)
	apply := flags.Bool("apply-system", false, "confirm authorized offline system preparation")
	check := flags.Bool("check-system", false, "inspect system migration history without bootstrap")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *apply == *check {
		return errors.New("select exactly one of --apply-system or --check-system")
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

	err := operatorprep.Prepare(operatorprep.Config{
		DatabaseURL:     os.Getenv("RUXBASE_PREPARE_DATABASE_URL"),
		LogsDatabaseURL: os.Getenv("RUXBASE_PREPARE_LOGS_URL"),
		ExpectedRole:    os.Getenv("RUXBASE_PREPARE_ROLE"),
		DataDir:         os.Getenv("RUXBASE_PREPARE_DATA_DIR"),
		EncryptionEnv:   os.Getenv("RUXBASE_PREPARE_ENCRYPTION_ENV"),
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
