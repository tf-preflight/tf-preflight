package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tf-preflight/tf-preflight/internal/discovery"
	"github.com/tf-preflight/tf-preflight/internal/model"
)

const summaryMaxCandidates = 10

func isTTYStdin() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

func interactiveConfigFlow(reader io.Reader, writer io.Writer, tfDir string, hclCtx *discovery.HCLContext) (string, bool, []model.Finding, error) {
	out := writer
	in := bufio.NewReader(reader)

	fmt.Fprintln(out, "Interactive preflight scan enabled.")
	printDiscoveredModuleContext(out, hclCtx)

	planPath := ""
	autoPlan := false
	choice, err := promptChoice(in, out, "Choose scan mode:\n1) Use existing plan\n2) Generate plan automatically\n3) Abort\n> ", 1, 3)
	if err != nil {
		return "", false, hclCtx.Findings, err
	}
	switch choice {
	case 1:
		selectedPlan, err := choosePlanPath(in, out, tfDir)
		if err != nil {
			return "", false, hclCtx.Findings, err
		}
		planPath = selectedPlan
	case 2:
		autoPlan = true
	case 3:
		return "", false, hclCtx.Findings, errInteractiveCancel
	}

	filteredFindings, err := applyModuleFindingChoices(in, out, hclCtx.Findings)
	if err != nil {
		return "", false, hclCtx.Findings, err
	}
	return planPath, autoPlan, filteredFindings, nil
}

func printDiscoveredModuleContext(writer io.Writer, hclCtx *discovery.HCLContext) {
	fmt.Fprintf(writer, "Scanning directory: %s\n", hclCtx.RootDir)
	fmt.Fprintf(writer, "Module imports discovered: %d\n", len(hclCtx.ModuleImports))
	for _, module := range hclCtx.ModuleImports {
		fmt.Fprintf(writer, "  - %s (source: %s)\n", module.Name, module.Source)
	}
	if len(hclCtx.Findings) == 0 {
		fmt.Fprintln(writer, "No module validation findings were detected.")
		return
	}
	fmt.Fprintln(writer, "Module validation findings:")
	for _, finding := range hclCtx.Findings {
		fmt.Fprintf(writer, "  [%s] %s: %s\n", finding.Severity, finding.Code, finding.Message)
	}
}

func choosePlanPath(reader *bufio.Reader, writer io.Writer, tfDir string) (string, error) {
	plans, err := discoverPlanCandidates(tfDir)
	if err != nil {
		return "", err
	}

	if len(plans) == 0 {
		return chooseManualPlanPath(reader, writer, "No plan files found in directory.", tfDir)
	}

	fmt.Fprintln(writer, "Detected plan candidates:")
	for idx, plan := range plans {
		fmt.Fprintf(writer, "  %d) %s\n", idx+1, relativeFrom(tfDir, plan))
	}
	fmt.Fprintln(writer, "  0) Enter plan path manually")
	for {
		choice, err := promptChoice(reader, writer, "Select a plan by number: ", 0, len(plans))
		if err != nil {
			return "", err
		}
		if choice == 0 {
			return chooseManualPlanPath(reader, writer, "Enter plan path: ", tfDir)
		}
		return plans[choice-1], nil
	}
}

func chooseManualPlanPath(reader *bufio.Reader, writer io.Writer, prompt, tfDir string) (string, error) {
	promptText := prompt
	if promptText == "" {
		promptText = "Enter plan path: "
	}
	for {
		value, err := promptLine(reader, writer, promptText)
		if err != nil {
			return "", err
		}
		planPath, err := resolveCandidatePlanPath(value, tfDir)
		if err != nil {
			fmt.Fprintln(writer, err)
			continue
		}
		return planPath, nil
	}
}

func resolveCandidatePlanPath(raw string, tfDir string) (string, error) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return "", errors.New("plan path cannot be empty")
	}
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Clean(filepath.Join(tfDir, cleaned))
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		return "", fmt.Errorf("plan path not found: %s", cleaned)
	}
	if info.IsDir() {
		return "", fmt.Errorf("plan path is a directory: %s", cleaned)
	}
	return cleaned, nil
}

func discoverPlanCandidates(tfDir string) ([]string, error) {
	plans := []string{}
	err := filepath.WalkDir(tfDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".terraform" || d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if isCandidatePlanFile(path) {
			plans = append(plans, path)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return nil, err
	}
	sort.Strings(plans)
	return plans, nil
}

func isCandidatePlanFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".tfplan") ||
		strings.HasSuffix(lower, ".tfplan.json") ||
		strings.HasSuffix(lower, ".plan") ||
		(strings.HasSuffix(lower, ".json") && strings.Contains(filepath.Base(lower), "plan"))
}

func applyModuleFindingChoices(reader *bufio.Reader, writer io.Writer, findings []model.Finding) ([]model.Finding, error) {
	warnings := []model.Finding{}
	for _, finding := range findings {
		if strings.HasPrefix(finding.Code, "MODULE_") && finding.Severity == "warn" {
			warnings = append(warnings, finding)
		}
	}
	if len(warnings) == 0 {
		return findings, nil
	}
	fmt.Fprintf(writer, "Module warning findings (%d):\n", len(warnings))
	for idx, finding := range warnings {
		fmt.Fprintf(writer, "  %d) %s: %s\n", idx+1, finding.Code, finding.Message)
	}
	for {
		raw, err := promptLine(reader, writer, "Enter warning indexes or codes to ignore (comma/space separated, empty keeps all): ")
		if err != nil {
			return nil, err
		}
		ignoredIndexes, ignoredCodes, err := parseIgnoreSelection(raw, len(warnings), warnings)
		if err != nil {
			fmt.Fprintln(writer, err)
			continue
		}
		return filterModuleWarningFindings(findings, warnings, ignoredIndexes, ignoredCodes), nil
	}
}

func filterModuleWarningFindings(all []model.Finding, warnings []model.Finding, ignoreIndexes map[int]struct{}, ignoreCodes map[string]struct{}) []model.Finding {
	ignoreGlobalIndexes := map[int]struct{}{}
	for idx, warning := range warnings {
		if _, ok := ignoreIndexes[idx+1]; ok {
			ignoreGlobalIndexes[idx] = struct{}{}
		}
		if _, ok := ignoreCodes[warning.Code]; ok {
			ignoreGlobalIndexes[idx] = struct{}{}
		}
	}
	filtered := make([]model.Finding, 0, len(all))
	warningOrdinal := 0
	for _, finding := range all {
		if strings.HasPrefix(finding.Code, "MODULE_") && finding.Severity == "warn" {
			if _, ok := ignoreGlobalIndexes[warningOrdinal]; ok {
				warningOrdinal++
				continue
			}
			warningOrdinal++
		}
		filtered = append(filtered, finding)
	}
	return filtered
}

func parseIgnoreSelection(raw string, maxIndex int, warnings []model.Finding) (map[int]struct{}, map[string]struct{}, error) {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return map[int]struct{}{}, map[string]struct{}{}, nil
	}
	ignoreIndexes := map[int]struct{}{}
	ignoreCodes := map[string]struct{}{}
	codeSet := map[string]struct{}{}
	for _, finding := range warnings {
		if finding.Code != "" {
			codeSet[finding.Code] = struct{}{}
		}
	}

	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if idx, err := strconv.Atoi(part); err == nil {
			if idx < 1 || idx > maxIndex {
				return nil, nil, fmt.Errorf("warning index %d is out of range", idx)
			}
			ignoreIndexes[idx] = struct{}{}
			continue
		}
		if _, ok := codeSet[strings.TrimSpace(part)]; !ok {
			return nil, nil, fmt.Errorf("unknown module warning code: %s", part)
		}
		ignoreCodes[strings.TrimSpace(part)] = struct{}{}
	}
	return ignoreIndexes, ignoreCodes, nil
}

func confirmInteractiveRun(reader *bufio.Reader, writer io.Writer, tfDir, planPath string, opts model.CommandOptions, subscriptionID string, candidates []model.Candidate, findings []model.Finding) error {
	out := writer
	moduleErrors := 0
	moduleWarnings := 0
	for _, finding := range findings {
		if strings.HasPrefix(finding.Code, "MODULE_") {
			switch finding.Severity {
			case "error":
				moduleErrors++
			case "warn":
				moduleWarnings++
			}
		}
	}

	if opts.Output == "text" {
		printPreflightPlanSummary(out, tfDir, planPath, firstNonEmptyString(subscriptionID), opts, candidates, findings)
		if moduleErrors > 0 || moduleWarnings > 0 {
			fmt.Fprintf(out, "  Module findings: %d errors, %d warnings\n", moduleErrors, moduleWarnings)
		}
		fmt.Fprintf(out, "  Severity threshold: %s\n", opts.SeverityThreshold)
	}

	ok, err := promptYesNo(reader, out, "Proceed with scan?", true)
	if err != nil {
		return err
	}
	if !ok {
		return errInteractiveCancel
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "unknown"
}

func printPreflightPlanSummary(out io.Writer, tfDir, planPath, resolvedSubscription string, opts model.CommandOptions, candidates []model.Candidate, findings []model.Finding) {
	planSource := planPath
	if opts.AutoPlan {
		planSource = "auto-generated terraform plan"
	}

	fmt.Fprintln(out, "Preflight execution summary:")
	fmt.Fprintf(out, "  Directory: %s\n", tfDir)
	fmt.Fprintf(out, "  Plan: %s\n", planSource)
	fmt.Fprintf(out, "  Subscription: %s\n", resolvedSubscription)
	fmt.Fprintf(out, "  Severity threshold: %s\n", opts.SeverityThreshold)
	fmt.Fprintf(out, "  Candidates: %d\n", len(candidates))

	actionCounts, typeCounts := summarizeCandidateCounts(candidates)
	fmt.Fprintf(out, "  Action counts: %s\n", renderCounts(actionCounts))
	fmt.Fprintf(out, "  Resource types: %s\n", renderCounts(typeCounts))

	fmt.Fprintln(out, "  Key candidate preview:")
	for i, candidate := range candidates {
		if i >= summaryMaxCandidates {
			remaining := len(candidates) - summaryMaxCandidates
			if remaining > 0 {
				fmt.Fprintf(out, "    ... and %d more candidate(s)\n", remaining)
			}
			break
		}

		tags := candidateWarningTags(candidate)
		tagText := ""
		if len(tags) > 0 {
			tagText = " [" + strings.Join(tags, ", ") + "]"
		}
		location := firstNonEmptyString(candidate.Location, "<unknown>")
		resourceGroup := firstNonEmptyString(candidate.ResourceGroup, "<unknown>")
		subscription := firstNonEmptyString(candidate.SubscriptionID, resolvedSubscription, "<unknown>")
		fmt.Fprintf(out, "    %2d) %s (%s, %s) at %s | rg=%s | sub=%s | src=%s%s\n", i+1, candidate.Address, candidate.ResourceType, candidate.Action, location, resourceGroup, subscription, candidate.Source, tagText)
	}

	errorLines := errorSummaryLines(findings)
	fmt.Fprintf(out, "  Blocking findings: %d\n", len(errorLines))
	for _, line := range errorLines {
		fmt.Fprintf(out, "    - %s\n", line)
	}

	warnings := warningSummaryLines(findings)
	if len(warnings) > 0 {
		show := minInt(len(warnings), 5)
		fmt.Fprintf(out, "  Optional warnings (showing %d/%d):\n", show, len(warnings))
		for _, line := range warnings[:show] {
			fmt.Fprintf(out, "    - %s\n", line)
		}
		if len(warnings) > show {
			fmt.Fprintf(out, "    ... and %d more warning(s)\n", len(warnings)-show)
		}
	}
}

func summarizeCandidateCounts(candidates []model.Candidate) (map[string]int, map[string]int) {
	actionCounts := map[string]int{}
	typeCounts := map[string]int{}
	for _, c := range candidates {
		actionCounts[c.Action]++
		typeCounts[c.ResourceType]++
	}
	return actionCounts, typeCounts
}

func renderCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}

func candidateWarningTags(c model.Candidate) []string {
	tags := []string{}
	if c.PlanUnknown {
		tags = append(tags, "plan-unknown")
	}
	if len(c.Warnings) > 0 {
		tags = append(tags, fmt.Sprintf("warnings=%d", len(c.Warnings)))
	}
	return tags
}

func errorSummaryLines(findings []model.Finding) []string {
	lines := []string{}
	for _, finding := range findings {
		if strings.EqualFold(strings.TrimSpace(finding.Severity), "error") {
			if finding.Resource != "" {
				lines = append(lines, fmt.Sprintf("%s [%s] %s", finding.Code, finding.Resource, finding.Message))
			} else {
				lines = append(lines, fmt.Sprintf("%s: %s", finding.Code, finding.Message))
			}
		}
	}
	return lines
}

func warningSummaryLines(findings []model.Finding) []string {
	lines := []string{}
	for _, finding := range findings {
		if strings.EqualFold(strings.TrimSpace(finding.Severity), "warn") {
			if finding.Resource != "" {
				lines = append(lines, fmt.Sprintf("%s [%s] %s", finding.Code, finding.Resource, finding.Message))
			} else {
				lines = append(lines, fmt.Sprintf("%s: %s", finding.Code, finding.Message))
			}
		}
	}
	return lines
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func promptChoice(reader *bufio.Reader, writer io.Writer, prompt string, min, max int) (int, error) {
	for {
		value, err := promptLine(reader, writer, prompt)
		if err != nil {
			return 0, err
		}
		choice, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && choice >= min && choice <= max {
			return choice, nil
		}
		fmt.Fprintf(writer, "Invalid choice. Enter a number between %d and %d.\n", min, max)
	}
}

func promptYesNo(reader *bufio.Reader, writer io.Writer, prompt string, defaultYes bool) (bool, error) {
	yr := "y"
	if defaultYes {
		yr = "Y"
	}
	promptLineText := fmt.Sprintf("%s (y/n, default=%s): ", prompt, strings.ToLower(yr))
	for {
		raw, err := promptLine(reader, writer, promptLineText)
		if err != nil {
			return false, err
		}
		trimmed := strings.ToLower(strings.TrimSpace(raw))
		switch trimmed {
		case "":
			return defaultYes, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(writer, "Enter 'y' or 'n'.")
		}
	}
}

func promptLine(reader *bufio.Reader, writer io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(writer, prompt); err != nil {
		return "", err
	}
	value, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return strings.TrimSpace(value), nil
		}
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func relativeFrom(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}
