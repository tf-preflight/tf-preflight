package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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
		// continue
	default:
		fmt.Printf("unknown command: %s\n", os.Args[1])
		fmt.Println(cliUsage)
		os.Exit(2)
	}

	fs := flag.NewFlagSet("scan", flag.ExitOnError)
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
	_ = fs.Parse(os.Args[2:])
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
			fmt.Println("Cannot run --interactive without a TTY stdin. Use --plan or --auto-plan with the non-interactive mode.")
			os.Exit(2)
		}
		if *tfDir == "" {
			*tfDir = "."
		}
	}

	if *tfDir == "" {
		fmt.Println("--tf-dir is required unless --interactive is enabled")
		os.Exit(2)
	}
	if !*interactive && !*autoPlan && *planPath == "" {
		fmt.Println("--plan or --auto-plan is required when not using --interactive")
		os.Exit(2)
	}
	if *threshold != "warn" && *threshold != "error" {
		fmt.Println("--severity-threshold must be warn or error")
		os.Exit(2)
	}
	if *output != "text" && *output != "json" {
		fmt.Println("--output must be text or json")
		os.Exit(2)
	}

	if err := run(model.CommandOptions{
		TfDir:             *tfDir,
		PlanPath:          *planPath,
		AutoPlan:          *autoPlan,
		Interactive:       *interactive,
		SubscriptionID:    *subscription,
		SeverityThreshold: *threshold,
		Output:            *output,
		ReportPath:        *reportPath,
		Verbose:           *verbose,
	}); err != nil {
		if errors.Is(err, errChecksFailed) {
			os.Exit(1)
		}
		if errors.Is(err, errUsage) {
			fmt.Println(err)
			os.Exit(2)
		}
		if errors.Is(err, errInteractiveCancel) {
			fmt.Println(err)
			os.Exit(2)
		}
		fmt.Println(err)
		os.Exit(1)
	}
}

var errUsage = errors.New("invalid command usage")
var errChecksFailed = errors.New("checks failed")
var errInteractiveCancel = errors.New("interactive scan cancelled")

func run(opts model.CommandOptions) error {
	ctx := context.Background()
	progress := ui.NewProgress(opts.Output == "text", opts.Verbose, os.Stdout)
	progress.Start("preparing workspace", 1)

	absDir, err := filepath.Abs(opts.TfDir)
	if err != nil {
		return err
	}
	progress.Tick("loading Terraform directory")

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
	progress.Tick("terraform directory parsed")

	planData, finalPlanPath, err := resolvePlan(ctx, opts, absDir)
	if err != nil {
		return fmt.Errorf("failed resolving plan: %w", err)
	}
	progress.Tick("plan data loaded")

	candidates, err := discovery.CandidatesFromPlan(planData, hclCtx)
	if err != nil {
		return fmt.Errorf("failed reading plan: %w", err)
	}

	subscriptionID := opts.SubscriptionID
	if subscriptionID == "" {
		subscriptionID = hclCtx.Subscription
	}
	if subscriptionID == "" {
		subscriptionID = strings.TrimSpace(os.Getenv("ARM_SUBSCRIPTION_ID"))
	}
	if subscriptionID == "" {
		subscriptionID = strings.TrimSpace(os.Getenv("AZURE_SUBSCRIPTION_ID"))
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

	progress.Tick("candidate set prepared")
	progress.Tick(fmt.Sprintf("resolved %d candidate resource(s)", len(candidates)))

	token, err := resolveAzureToken(opts.Verbose)
	if err != nil {
		return fmt.Errorf("azure token missing and CLI token lookup failed: %w", err)
	}

	client := azure.NewAzureClient(token)
	findings, err := azure.RunChecks(ctx, candidates, client, subscriptionID, opts.SeverityThreshold, progress)
	if err != nil {
		return err
	}
	progress.Tick("azure checks completed")
	findings = append(hclCtx.Findings, findings...)

	progress.Message("building report")
	reportObj := report.BuildReport(absDir, finalPlanPath, opts.AutoPlan, subscriptionID, candidates, findings)
	reportObj.Findings = report.SortedFindings(findings)
	progress.Done("ready")

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
			if isJSONPlan(data) {
				if opts.Verbose {
					fmt.Fprintf(os.Stdout, "Using existing JSON plan: %s\n", p)
				}
				return data, p, nil
			}
			return nil, p, fmt.Errorf("plan file is not valid json")
		}
		if opts.Verbose {
			fmt.Fprintf(os.Stdout, "Converting binary plan to JSON: %s\n", p)
		}
		return terraformShowJSON(ctx, p, opts.Verbose)
	}

	if opts.Verbose {
		fmt.Fprintf(os.Stdout, "Generating plan in: %s\n", tfDir)
	}
	planPath, err := runTerraformPlan(ctx, tfDir, opts.Verbose)
	if err != nil {
		return nil, "", err
	}
	return terraformShowJSON(ctx, planPath, opts.Verbose)
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

func terraformShowJSON(ctx context.Context, planPath string, verbose bool) ([]byte, string, error) {
	cmd := exec.CommandContext(ctx, "terraform", "show", "-json", planPath)
	cmd.Dir = filepath.Dir(planPath)
	if verbose {
		cmd.Stderr = os.Stderr
	}
	data, err := cmd.Output()
	if err != nil {
		return nil, planPath, err
	}
	return data, planPath, nil
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

func isJSONPlan(data []byte) bool {
	var js map[string]any
	return json.Unmarshal(data, &js) == nil
}
