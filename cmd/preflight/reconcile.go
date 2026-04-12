package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tf-preflight/tf-preflight/internal/azure"
	"github.com/tf-preflight/tf-preflight/internal/discovery"
	"github.com/tf-preflight/tf-preflight/internal/model"
	"github.com/tf-preflight/tf-preflight/internal/reconcile"
	"github.com/tf-preflight/tf-preflight/internal/report"
	"github.com/tf-preflight/tf-preflight/internal/ui"
)

func runReconcile(opts model.CommandOptions) error {
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
	progress.Message("candidate set prepared")

	subscriptionID, err := resolveSubscriptionID(opts, hclCtx)
	if err != nil {
		return err
	}
	for i := range candidates {
		if candidates[i].SubscriptionID == "" {
			candidates[i].SubscriptionID = subscriptionID
		}
	}

	token, err := resolveAzureToken(opts.Verbose)
	if err != nil {
		return fmt.Errorf("azure token missing and CLI token lookup failed: %w", err)
	}

	client := azure.NewAzureClient(token)
	result, err := reconcile.Run(ctx, candidates, client, subscriptionID, absDir, progress)
	if err != nil {
		return err
	}
	progress.Start("finalizing report", 1)

	reportObj := report.BuildReconcileReport(absDir, finalPlanPath, opts.AutoPlan, subscriptionID, len(candidates), result.EvaluatedCandidates, result.Findings, result.Recommendations)
	progress.Done("report ready")

	if opts.Output == "json" {
		if err := report.WriteReconcileJSON(reportObj, selectReportPath(opts)); err != nil {
			return err
		}
	} else {
		report.WriteReconcileText(reportObj)
		if opts.ReportPath != "" {
			if err := report.WriteReconcileJSON(reportObj, opts.ReportPath); err != nil {
				return err
			}
		}
	}

	if report.IsReconcileFailure(reportObj.Findings) {
		return errChecksFailed
	}
	return nil
}

func resolveSubscriptionID(opts model.CommandOptions, hclCtx *discovery.HCLContext) (string, error) {
	subscriptionID := opts.SubscriptionID
	if subscriptionID == "" && hclCtx != nil {
		subscriptionID = hclCtx.Subscription
	}
	if subscriptionID == "" {
		subscriptionID = strings.TrimSpace(os.Getenv("ARM_SUBSCRIPTION_ID"))
	}
	if subscriptionID == "" {
		subscriptionID = strings.TrimSpace(os.Getenv("AZURE_SUBSCRIPTION_ID"))
	}
	if subscriptionID != "" {
		return subscriptionID, nil
	}

	subscriptionID, err := azure.ResolveSubscriptionFromCLI()
	if err != nil {
		return "", fmt.Errorf("subscription could not be resolved: %w", err)
	}
	return subscriptionID, nil
}
