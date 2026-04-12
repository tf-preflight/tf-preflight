package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/tf-preflight/tf-preflight/internal/model"
)

func parseScanOptions(args []string) (model.CommandOptions, error) {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	tfDir := fs.String("tf-dir", "", "Terraform directory")
	planPath := fs.String("plan", "", "Terraform plan file")
	autoPlan := fs.Bool("auto-plan", false, "Generate plan automatically")
	interactive := fs.Bool("interactive", false, "Run guided interactive scan flow")
	subscription := fs.String("subscription-id", "", "Subscription override")
	threshold := fs.String("severity-threshold", "error", "warn or error")
	output := fs.String("output", "text", "text|json")
	reportPath := fs.String("report-path", "", "Where to write JSON output")
	verbose := fs.Bool("verbose", false, "Print detailed runtime output")
	verboseSet := false

	if err := fs.Parse(args); err != nil {
		return model.CommandOptions{}, fmt.Errorf("%w: %v", errUsage, err)
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "verbose" {
			verboseSet = true
		}
	})

	if *interactive && !verboseSet {
		*verbose = true
	}
	if *interactive {
		if !isTTYStdin() {
			return model.CommandOptions{}, fmt.Errorf("%w: cannot run --interactive without a TTY stdin. Use --plan or --auto-plan with the non-interactive mode", errUsage)
		}
		if *tfDir == "" {
			*tfDir = "."
		}
	}
	if *tfDir == "" {
		return model.CommandOptions{}, fmt.Errorf("%w: --tf-dir is required unless --interactive is enabled", errUsage)
	}
	if !*interactive && !*autoPlan && *planPath == "" {
		return model.CommandOptions{}, fmt.Errorf("%w: --plan or --auto-plan is required when not using --interactive", errUsage)
	}
	if *threshold != "warn" && *threshold != "error" {
		return model.CommandOptions{}, fmt.Errorf("%w: --severity-threshold must be warn or error", errUsage)
	}
	if *output != "text" && *output != "json" {
		return model.CommandOptions{}, fmt.Errorf("%w: --output must be text or json", errUsage)
	}

	return model.CommandOptions{
		TfDir:             *tfDir,
		PlanPath:          *planPath,
		AutoPlan:          *autoPlan,
		Interactive:       *interactive,
		SubscriptionID:    *subscription,
		SeverityThreshold: *threshold,
		Output:            *output,
		ReportPath:        *reportPath,
		Verbose:           *verbose,
	}, nil
}

func parseReconcileOptions(args []string) (model.CommandOptions, error) {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	tfDir := fs.String("tf-dir", "", "Terraform directory")
	planPath := fs.String("plan", "", "Terraform plan file")
	autoPlan := fs.Bool("auto-plan", false, "Generate plan automatically")
	subscription := fs.String("subscription-id", "", "Subscription override")
	output := fs.String("output", "text", "text|json")
	reportPath := fs.String("report-path", "", "Where to write JSON output")
	verbose := fs.Bool("verbose", false, "Print detailed runtime output")

	if err := fs.Parse(args); err != nil {
		return model.CommandOptions{}, fmt.Errorf("%w: %v", errUsage, err)
	}
	if *tfDir == "" {
		return model.CommandOptions{}, fmt.Errorf("%w: --tf-dir is required", errUsage)
	}
	if !*autoPlan && *planPath == "" {
		return model.CommandOptions{}, fmt.Errorf("%w: --plan or --auto-plan is required", errUsage)
	}
	if *output != "text" && *output != "json" {
		return model.CommandOptions{}, fmt.Errorf("%w: --output must be text or json", errUsage)
	}

	return model.CommandOptions{
		TfDir:          *tfDir,
		PlanPath:       *planPath,
		AutoPlan:       *autoPlan,
		SubscriptionID: *subscription,
		Output:         *output,
		ReportPath:     *reportPath,
		Verbose:        *verbose,
	}, nil
}

func exitCodeForError(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, errUsage) || errors.Is(err, errInteractiveCancel) {
		return 2
	}
	return 1
}
