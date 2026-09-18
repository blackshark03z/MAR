package main

import (
	"flag"

	"mar/internal/retention"
)

func runRetentionPrune(args []string) error {
	fs := flag.NewFlagSet("retention-prune", flag.ContinueOnError)
	dataRoot := fs.String("data-root", ".mar", "MAR managed data root")
	keepActivation := fs.Int("keep-activation", 5, "number of newest marked activation backups to retain")
	if err := fs.Parse(args); err != nil {
		return err
	}
	result, err := retention.Prune(*dataRoot, *keepActivation)
	if err != nil {
		return err
	}
	return printJSON(result)
}
