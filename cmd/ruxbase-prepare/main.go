package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/AbdullahZaher/ruxbase/internal/operatorprep"
)

func run(args []string) error {
	flags := flag.NewFlagSet("ruxbase-prepare", flag.ContinueOnError)
	apply := flags.Bool("apply-system", false, "confirm authorized offline system preparation")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || !*apply {
		return errors.New("explicit --apply-system confirmation required")
	}

	return operatorprep.Prepare(operatorprep.Config{
		DatabaseURL:     os.Getenv("RUXBASE_PREPARE_DATABASE_URL"),
		LogsDatabaseURL: os.Getenv("RUXBASE_PREPARE_LOGS_URL"),
		ExpectedRole:    os.Getenv("RUXBASE_PREPARE_ROLE"),
		DataDir:         os.Getenv("RUXBASE_PREPARE_DATA_DIR"),
		EncryptionEnv:   os.Getenv("RUXBASE_PREPARE_ENCRYPTION_ENV"),
	})
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("System preparation completed; serving and user migrations not started.")
}
