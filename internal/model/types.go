package model

import "time"

// Candidate represents an Azure resource intent discovered from HCL or plan JSON.
type Candidate struct {
	Address         string         `json:"address"`
	ResourceType    string         `json:"resource_type"`
	Mode            string         `json:"mode,omitempty"`
	Action          string         `json:"action"`
	Location        string         `json:"location,omitempty"`
	SubscriptionID  string         `json:"subscription_id,omitempty"`
	ResourceGroup   string         `json:"resource_group,omitempty"`
	Name            string         `json:"name,omitempty"`
	Sku             string         `json:"sku,omitempty"`
	Namespace       string         `json:"namespace"`
	Source          string         `json:"source"`
	RawRestrictions map[string]any `json:"restrictions,omitempty"`
	PlanUnknown     bool           `json:"plan_unknown,omitempty"`
	Warnings        []string       `json:"warnings,omitempty"`
}

// ModuleImport summarizes a Terraform module block in root configuration.
type ModuleImport struct {
	Name         string `json:"name"`
	Source       string `json:"source"`
	SourceKind   string `json:"source_kind"`
	File         string `json:"file"`
	ResolvedPath string `json:"resolved_path"`
}

// Finding is a rule outcome for the preflight process.
type Finding struct {
	Severity string         `json:"severity"`
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Resource string         `json:"resource,omitempty"`
	Category string         `json:"category,omitempty"`
	Detail   map[string]any `json:"detail,omitempty"`
}

// Report is the machine-readable output used in CI.
type Report struct {
	GeneratedAt  time.Time   `json:"generated_at"`
	TfDirectory  string      `json:"tf_directory"`
	PlanPath     string      `json:"plan_path,omitempty"`
	AutoPlan     bool        `json:"auto_plan"`
	Subscription string      `json:"subscription"`
	Summary      Summary     `json:"summary"`
	Decision     Decision    `json:"decision"`
	Candidates   []Candidate `json:"candidates"`
	Findings     []Finding   `json:"findings"`
}

type Summary struct {
	TotalCandidates int `json:"total_candidates"`
	Actions         struct {
		Create int `json:"create"`
		Update int `json:"update"`
		Delete int `json:"delete"`
		Noop   int `json:"noop"`
	} `json:"actions"`
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
}

type Decision struct {
	Result     string `json:"result"`
	Deployable bool   `json:"deployable"`
	Confidence string `json:"confidence"`
	Blockers   int    `json:"blockers"`
	Degraded   int    `json:"degraded"`
	Advisories int    `json:"advisories"`
}

type ImportRecommendation struct {
	TerraformAddress string `json:"terraform_address"`
	ResourceType     string `json:"resource_type"`
	ImportID         string `json:"import_id"`
	WorkingDirectory string `json:"working_directory"`
	Command          string `json:"command"`
}

type ReconcileReport struct {
	GeneratedAt     time.Time              `json:"generated_at"`
	TfDirectory     string                 `json:"tf_directory"`
	PlanPath        string                 `json:"plan_path,omitempty"`
	AutoPlan        bool                   `json:"auto_plan"`
	Subscription    string                 `json:"subscription"`
	Summary         ReconcileSummary       `json:"summary"`
	Findings        []Finding              `json:"findings"`
	Recommendations []ImportRecommendation `json:"recommendations"`
}

type ReconcileSummary struct {
	TotalCandidates     int `json:"total_candidates"`
	EvaluatedCandidates int `json:"evaluated_candidates"`
	ImportRequired      int `json:"import_required"`
	Errors              int `json:"errors"`
	Warnings            int `json:"warnings"`
}

// CommandOptions captures CLI controls.
type CommandOptions struct {
	TfDir             string
	PlanPath          string
	AutoPlan          bool
	Interactive       bool
	SubscriptionID    string
	SeverityThreshold string
	Output            string
	ReportPath        string
	Verbose           bool
}
