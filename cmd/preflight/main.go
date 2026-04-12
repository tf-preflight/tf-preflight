package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tf-preflight/tf-preflight/internal/azure"
	"github.com/tf-preflight/tf-preflight/internal/discovery"
	"github.com/tf-preflight/tf-preflight/internal/model"
	"github.com/tf-preflight/tf-preflight/internal/report"
	"github.com/tf-preflight/tf-preflight/internal/ui"
)

var (
	version           = "dev"
	gitCommit         = "dirty"
	buildDate         = "unknown"
	azureQueryBackend = "Azure REST (management.azure.com)"
	hclLibVersion     = "github.com/hashicorp/hcl/v2@v2.23.0"
	ctyLibVersion     = "github.com/zclconf/go-cty@v1.15.1"
	goVersion         = "go1.21"
)

var cliUsage = `
Usage:
  tf-preflight scan --tf-dir <path> [--plan <plan-path> | --auto-plan | --interactive]
  tf-preflight reconcile --tf-dir <path> [--plan <plan-path> | --auto-plan]
  tf-preflight version

Flags:
  --tf-dir            Terraform directory
  --plan              Path to tfplan file (binary .tfplan) or json plan
  --auto-plan         Run terraform init + plan -out internally
  --interactive       Run guided prompts for directory, plan source, and module findings
  --subscription-id   Optional subscription override
  --severity-threshold warn|error
  --output            text|json
  --report-path       Optional JSON report path
  --verbose           Print detailed runtime output
  tf-preflight version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Println(cliUsage)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "version":
		printVersion()
		return
	case "--version", "-v":
		printVersion()
		return
	case "scan":
		opts, err := parseScanOptions(os.Args[2:])
		if err != nil {
			fmt.Println(err)
			os.Exit(exitCodeForError(err))
		}
		if err := runScan(opts); err != nil {
			if errors.Is(err, errUsage) || errors.Is(err, errInteractiveCancel) {
				fmt.Println(err)
			} else {
				fmt.Println(err)
			}
			os.Exit(exitCodeForError(err))
		}
		return
	case "reconcile":
		opts, err := parseReconcileOptions(os.Args[2:])
		if err != nil {
			fmt.Println(err)
			os.Exit(exitCodeForError(err))
		}
		if err := runReconcile(opts); err != nil {
			fmt.Println(err)
			os.Exit(exitCodeForError(err))
		}
		return
	default:
		fmt.Printf("unknown command: %s\n", os.Args[1])
		fmt.Println(cliUsage)
		os.Exit(2)
	}
}

var errUsage = errors.New("invalid command usage")
var errChecksFailed = errors.New("checks failed")
var errInteractiveCancel = errors.New("interactive scan cancelled")

func runScan(opts model.CommandOptions) error {
	ctx := context.Background()
	progress := ui.NewProgress(opts.Output == "text", opts.Verbose, os.Stdout)
	progress.Message("preparing workspace")

	absDir, err := filepath.Abs(opts.TfDir)
	if err != nil {
		return err
	}
	progress.Message("loading Terraform directory")

	hclCtx, err := discovery.ParseDirectory(absDir)
	if err != nil {
		return fmt.Errorf("failed parsing HCL: %w", err)
	}
	if opts.Interactive {
		selectedPlanPath, selectedAutoPlan, filteredFindings, err := interactiveConfigFlow(os.Stdin, os.Stdout, absDir, hclCtx)
		if err != nil {
			return err
		}
		opts.PlanPath = selectedPlanPath
		opts.AutoPlan = selectedAutoPlan
		hclCtx.Findings = filteredFindings
	}
	if len(hclCtx.Findings) > 0 {
		progress.Message(fmt.Sprintf("hcl parsing produced %d module validation note(s)", len(hclCtx.Findings)))
	}
	progress.Message("terraform directory parsed")

	planData, finalPlanPath, err := resolvePlan(ctx, opts, absDir)
	if err != nil {
		return fmt.Errorf("failed resolving plan: %w", err)
	}
	progress.Message("plan data loaded")

	candidates, err := discovery.CandidatesFromPlan(planData, hclCtx)
	if err != nil {
		return fmt.Errorf("failed reading plan: %w", err)
	}

	subscriptionID, err := resolveSubscriptionID(opts, hclCtx)
	if err != nil {
		return err
	}
	for i := range candidates {
		if candidates[i].SubscriptionID == "" {
			candidates[i].SubscriptionID = subscriptionID
		}
	}

	if opts.Output == "text" && !opts.Interactive {
		printPreflightPlanSummary(os.Stdout, absDir, finalPlanPath, firstNonEmptyString(subscriptionID), opts, candidates, hclCtx.Findings)
	}

	if opts.Interactive {
		if err := confirmInteractiveRun(bufio.NewReader(os.Stdin), os.Stdout, absDir, finalPlanPath, opts, subscriptionID, candidates, hclCtx.Findings); err != nil {
			return err
		}
	}

	progress.Message("candidate set prepared")
	progress.Message(fmt.Sprintf("resolved %d candidate resource(s)", len(candidates)))

	token, err := resolveAzureToken(opts.Verbose)
	if err != nil {
		return fmt.Errorf("azure token missing and CLI token lookup failed: %w", err)
	}

	client := azure.NewAzureClient(token)
	findings, err := azure.RunChecks(ctx, candidates, client, subscriptionID, opts.SeverityThreshold, progress)
	if err != nil {
		return err
	}
	progress.Message("azure checks completed")
	findings = append(hclCtx.Findings, findings...)

	progress.Start("finalizing report", 1)
	reportObj := report.BuildReport(absDir, finalPlanPath, opts.AutoPlan, subscriptionID, candidates, findings)
	progress.Done("report ready")

	if opts.Output == "json" {
		if err := report.WriteJSON(reportObj, selectReportPath(opts)); err != nil {
			return err
		}
	} else {
		report.WriteText(reportObj)
		if opts.ReportPath != "" {
			if err := report.WriteJSON(reportObj, opts.ReportPath); err != nil {
				return err
			}
		}
	}

	if report.IsFailure(findings, opts.SeverityThreshold) {
		return errChecksFailed
	}

	return nil
}

func selectReportPath(opts model.CommandOptions) string {
	return opts.ReportPath
}

func resolvePlan(ctx context.Context, opts model.CommandOptions, tfDir string) ([]byte, string, error) {
	if opts.PlanPath != "" {
		p := opts.PlanPath
		if !filepath.IsAbs(p) {
			p = filepath.Clean(filepath.Join(tfDir, p))
		}
		if strings.HasSuffix(strings.ToLower(p), ".json") {
			data, err := os.ReadFile(p)
			if err != nil {
				return nil, p, err
			}
			if len(data) == 0 {
				return nil, p, fmt.Errorf("plan file is empty")
			}
			if err := validateTerraformPlanJSON(data); err != nil {
				return nil, p, fmt.Errorf("plan file is valid JSON but not a Terraform plan JSON document: %w", err)
			}
			if opts.Verbose {
				fmt.Fprintf(os.Stdout, "Using existing JSON plan: %s\n", p)
			}
			return data, p, nil
		}
		if opts.Verbose {
			fmt.Fprintf(os.Stdout, "Converting binary plan to JSON: %s\n", p)
		}
		return terraformShowJSON(ctx, tfDir, p, opts.Verbose)
	}

	if opts.Verbose {
		fmt.Fprintf(os.Stdout, "Generating plan in: %s\n", tfDir)
	}
	planPath, err := runTerraformPlan(ctx, tfDir, opts.Verbose)
	if err != nil {
		return nil, "", err
	}
	return terraformShowJSON(ctx, tfDir, planPath, opts.Verbose)
}

func runTerraformPlan(ctx context.Context, tfDir string, verbose bool) (string, error) {
	if err := execCommand(ctx, tfDir, verbose, "terraform", "init", "-input=false", "-no-color"); err != nil {
		return "", err
	}
	planPath := filepath.Join(os.TempDir(), fmt.Sprintf("tf-preflight-%d.tfplan", time.Now().UnixNano()))
	if err := execCommand(ctx, tfDir, verbose, "terraform", "plan", "-input=false", "-no-color", "-out", planPath); err != nil {
		return "", err
	}
	return planPath, nil
}

func terraformShowJSON(ctx context.Context, tfDir, planPath string, verbose bool) ([]byte, string, error) {
	cmd := exec.CommandContext(ctx, "terraform", "show", "-json", planPath)
	cmd.Dir = terraformShowWorkDir(tfDir, planPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	if verbose {
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	} else {
		cmd.Stderr = &stderr
	}
	if err := cmd.Run(); err != nil {
		return nil, planPath, formatTerraformShowError(planPath, cmd.Dir, stderr.String(), err)
	}
	data := stdout.Bytes()
	if err := validateTerraformPlanJSON(data); err != nil {
		return nil, planPath, fmt.Errorf("terraform show output is not a Terraform plan JSON document: %w", err)
	}
	return data, planPath, nil
}

func terraformShowWorkDir(tfDir, planPath string) string {
	if strings.TrimSpace(tfDir) != "" {
		return tfDir
	}
	return filepath.Dir(planPath)
}

func formatTerraformShowError(planPath, workDir, stderr string, err error) error {
	trimmed := strings.TrimSpace(stderr)
	if isProviderSchemaLoadFailure(trimmed) {
		return fmt.Errorf("terraform show -json failed for plan %s while loading provider schemas from %s: %s", planPath, workDir, trimmed)
	}
	if trimmed != "" {
		return fmt.Errorf("terraform show -json failed for plan %s in %s: %s", planPath, workDir, trimmed)
	}
	return fmt.Errorf("terraform show -json failed for plan %s in %s: %w", planPath, workDir, err)
}

func isProviderSchemaLoadFailure(stderr string) bool {
	lower := strings.ToLower(strings.TrimSpace(stderr))
	return strings.Contains(lower, "failed to load plugin schemas") ||
		strings.Contains(lower, "failed to obtain provider schema") ||
		strings.Contains(lower, "provider schema") ||
		strings.Contains(lower, "unavailable provider")
}

func execCommand(ctx context.Context, dir string, verbose bool, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("command failed: %s %s", name, strings.Join(args, " "))
		}
		return nil
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command failed: %s %s\n%s", name, strings.Join(args, " "), out)
	}
	return nil
}

func azureTokenFromCLI(verbose bool) (string, error) {
	if verbose {
		fmt.Println("Obtaining token from azure cli: az account get-access-token")
	}
	cmd := exec.Command("az", "account", "get-access-token", "--resource", "https://management.azure.com", "--query", "accessToken", "-o", "tsv")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func resolveAzureToken(verbose bool) (string, error) {
	for _, envVar := range []string{"AZURE_ACCESS_TOKEN", "ARM_ACCESS_TOKEN", "AZURE_CLI_TOKEN"} {
		if token := strings.TrimSpace(os.Getenv(envVar)); token != "" {
			if verbose {
				fmt.Printf("Using token from %s\n", envVar)
			}
			return token, nil
		}
	}
	return azureTokenFromCLI(verbose)
}

func printVersion() {
	fmt.Printf("tf-preflight version: %s\n", version)
	if gitCommit != "" {
		fmt.Printf("git commit: %s\n", gitCommit)
	}
	if buildDate != "" {
		fmt.Printf("build date: %s\n", buildDate)
	}
	fmt.Printf("query backend: %s\n", azureQueryBackend)
	fmt.Printf("dependencies:\n")
	fmt.Printf("  hcl: %s\n", hclLibVersion)
	fmt.Printf("  cty: %s\n", ctyLibVersion)
	fmt.Printf("  go: %s\n", goVersion)
}

func validateTerraformPlanJSON(data []byte) error {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}

	rawFormatVersion, ok := envelope["format_version"]
	if !ok {
		return errors.New("missing format_version")
	}

	var formatVersion string
	if err := json.Unmarshal(rawFormatVersion, &formatVersion); err != nil || strings.TrimSpace(formatVersion) == "" {
		return errors.New("format_version must be a non-empty string")
	}

	for _, key := range []string{"resource_changes", "planned_values", "configuration"} {
		if _, ok := envelope[key]; ok {
			return nil
		}
	}

	return errors.New("missing Terraform plan sections (expected one of resource_changes, planned_values, configuration)")
}
