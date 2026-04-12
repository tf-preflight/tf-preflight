package discovery

import (
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/tf-preflight/tf-preflight/internal/model"
)

type HCLContext struct {
	Variables           map[string]any
	Locals              map[string]any
	Outputs             map[string]any
	OutputExprs         map[string]hcl.Expression
	ModuleValues        map[string]any
	Subscription        string
	RootDir             string
	AddressPrefix       string
	EachKey             string
	EachValue           any
	CandidateMap        map[string]model.Candidate
	TraversalCandidates map[string]model.Candidate
	Candidates          []model.Candidate
	ModuleImports       []model.ModuleImport
	ModuleCalls         []moduleCall
	Findings            []model.Finding
}

type moduleCall struct {
	ImportIndex int
	Name        string
	File        string
	Attributes  map[string]hcl.Expression
}

type parseOptions struct {
	AddressPrefix string
	SeedVariables map[string]any
	Subscription  string
	EachKey       string
	EachValue     any
}

// ParseDirectory extracts static Terraform intent from .tf files.
func ParseDirectory(root string) (*HCLContext, error) {
	return parseDirectory(root, parseOptions{})
}

func parseDirectory(root string, opts parseOptions) (*HCLContext, error) {
	ctx := &HCLContext{
		Variables:           cloneAnyMap(opts.SeedVariables),
		Locals:              map[string]any{},
		Outputs:             map[string]any{},
		OutputExprs:         map[string]hcl.Expression{},
		ModuleValues:        map[string]any{},
		Subscription:        opts.Subscription,
		RootDir:             root,
		AddressPrefix:       opts.AddressPrefix,
		EachKey:             opts.EachKey,
		EachValue:           opts.EachValue,
		CandidateMap:        map[string]model.Candidate{},
		TraversalCandidates: map[string]model.Candidate{},
		Findings:            []model.Finding{},
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	files := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".tf") {
			files = append(files, filepath.Join(root, entry.Name()))
		}
	}
	if err := loadAutoVariableFiles(root, ctx); err != nil {
		return nil, err
	}

	p := hclparse.NewParser()
	for _, path := range files {
		file, diags := p.ParseHCLFile(path)
		if diags.HasErrors() {
			return nil, diags
		}

		blockSchema := &hcl.BodySchema{
			Blocks: []hcl.BlockHeaderSchema{
				{Type: "resource", LabelNames: []string{"type", "name"}},
				{Type: "provider", LabelNames: []string{"name"}},
				{Type: "variable", LabelNames: []string{"name"}},
				{Type: "locals"},
				{Type: "module", LabelNames: []string{"name"}},
				{Type: "output", LabelNames: []string{"name"}},
			},
		}
		content, _, _ := file.Body.PartialContent(blockSchema)
		if content == nil {
			continue
		}

		parseVariables(content, ctx)
		parseLocals(content, ctx)
		parseProvider(content, ctx)
		parseResources(content, ctx)
		parseModules(content, ctx, path)
		parseOutputs(content, ctx)
	}

	validateModuleImports(ctx)
	if err := mergeLocalModuleCandidates(ctx); err != nil {
		return nil, err
	}
	evaluateOutputs(ctx)

	for _, c := range ctx.CandidateMap {
		ctx.Candidates = append(ctx.Candidates, c)
	}

	return ctx, nil
}

func parseVariables(content *hcl.BodyContent, ctx *HCLContext) {
	for _, b := range content.Blocks.OfType("variable") {
		if len(b.Labels) != 1 {
			continue
		}
		name := b.Labels[0]
		if _, exists := ctx.Variables[name]; exists {
			continue
		}
		content, _, _ := b.Body.PartialContent(&hcl.BodySchema{
			Attributes: []hcl.AttributeSchema{
				{Name: "default"},
			},
		})
		if content == nil {
			continue
		}
		if attr, ok := content.Attributes["default"]; ok {
			if val, ok := evalExpression(attr.Expr, ctx); ok {
				ctx.Variables[name] = val
			}
		}
	}
}

func parseLocals(content *hcl.BodyContent, ctx *HCLContext) {
	for _, b := range content.Blocks.OfType("locals") {
		attrs, diags := b.Body.JustAttributes()
		if diags.HasErrors() {
			// Ignore block-level diagnostics and keep best-effort attribute resolution.
		}
		if len(attrs) == 0 {
			continue
		}
		for key, attr := range attrs {
			if val, ok := evalExpression(attr.Expr, ctx); ok {
				ctx.Locals[key] = val
			}
		}
	}
}

func parseProvider(content *hcl.BodyContent, ctx *HCLContext) {
	for _, b := range content.Blocks.OfType("provider") {
		if len(b.Labels) != 1 || b.Labels[0] != "azurerm" {
			continue
		}
		content, _, _ := b.Body.PartialContent(&hcl.BodySchema{
			Attributes: []hcl.AttributeSchema{
				{Name: "subscription_id"},
			},
		})
		if content == nil {
			continue
		}
		if attr, ok := content.Attributes["subscription_id"]; ok {
			if val, ok := evalExpression(attr.Expr, ctx); ok {
				if s, ok := toString(val); ok {
					setContextSubscription(ctx, s)
				}
			}
		}
	}
}

func parseModules(content *hcl.BodyContent, ctx *HCLContext, filePath string) {
	for _, b := range content.Blocks.OfType("module") {
		if len(b.Labels) != 1 {
			continue
		}
		name := b.Labels[0]
		attrs, diags := b.Body.JustAttributes()
		if diags.HasErrors() || len(attrs) == 0 {
			continue
		}
		mod := model.ModuleImport{
			Name: name,
			File: filePath,
		}
		if attr, ok := attrs["source"]; ok {
			if v, ok := evalExpression(attr.Expr, ctx); ok {
				if s, ok := toString(v); ok {
					mod.Source = strings.TrimSpace(s)
					mod.SourceKind = classifyModuleSource(mod.Source)
					ctx.ModuleImports = append(ctx.ModuleImports, mod)
					ctx.ModuleCalls = append(ctx.ModuleCalls, moduleCall{
						ImportIndex: len(ctx.ModuleImports) - 1,
						Name:        name,
						File:        filePath,
						Attributes:  attributeExpressions(attrs),
					})
					continue
				}
			}
		}
		ctx.Findings = append(ctx.Findings, model.Finding{
			Severity: "warn",
			Code:     "MODULE_SOURCE_UNKNOWN",
			Message:  fmt.Sprintf("module %q source is dynamic or unsupported and could not be resolved statically", name),
			Resource: name,
		})
	}
}

func parseOutputs(content *hcl.BodyContent, ctx *HCLContext) {
	for _, b := range content.Blocks.OfType("output") {
		if len(b.Labels) != 1 {
			continue
		}
		name := b.Labels[0]
		blockContent, _, _ := b.Body.PartialContent(&hcl.BodySchema{
			Attributes: []hcl.AttributeSchema{
				{Name: "value"},
			},
		})
		if blockContent == nil {
			continue
		}
		if attr, ok := blockContent.Attributes["value"]; ok {
			ctx.OutputExprs[name] = attr.Expr
		}
	}
}

func evaluateOutputs(ctx *HCLContext) {
	if ctx == nil || len(ctx.OutputExprs) == 0 {
		return
	}
	names := make([]string, 0, len(ctx.OutputExprs))
	for name := range ctx.OutputExprs {
		names = append(names, name)
	}
	sort.Strings(names)
	for pass := 0; pass < 3; pass++ {
		for _, name := range names {
			if value, ok := evalExpression(ctx.OutputExprs[name], ctx); ok {
				ctx.Outputs[name] = value
			}
		}
	}
}

func classifyModuleSource(source string) string {
	switch {
	case strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") || strings.HasPrefix(source, "/"):
		return "local"
	case strings.HasPrefix(source, "git::") || strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://"):
		return "remote"
	case strings.Contains(source, "terraform.io/") || strings.HasPrefix(source, "registry.terraform.io/"):
		return "registry"
	default:
		return "other"
	}
}

func validateModuleImports(ctx *HCLContext) {
	referencedModules := map[string]struct{}{}
	for idx, m := range ctx.ModuleImports {
		if m.SourceKind != "local" {
			continue
		}
		if m.Source == "" {
			ctx.Findings = append(ctx.Findings, model.Finding{
				Severity: "warn",
				Code:     "MODULE_SOURCE_EMPTY",
				Message:  "module " + m.Name + " has empty source",
				Resource: m.Name,
			})
			continue
		}
		abs := filepath.Clean(filepath.Join(filepath.Dir(m.File), m.Source))
		m.ResolvedPath = abs
		ctx.ModuleImports[idx].ResolvedPath = abs

		info, err := os.Stat(abs)
		if err != nil {
			ctx.Findings = append(ctx.Findings, model.Finding{
				Severity: "error",
				Code:     "MODULE_SOURCE_NOT_FOUND",
				Message:  fmt.Sprintf("module %q source path does not exist: %s", m.Name, m.Source),
				Resource: m.Name,
				Detail: map[string]any{
					"source": m.Source,
					"path":   abs,
				},
			})
			continue
		}
		if !info.IsDir() {
			ctx.Findings = append(ctx.Findings, model.Finding{
				Severity: "error",
				Code:     "MODULE_SOURCE_INVALID",
				Message:  fmt.Sprintf("module %q source is not a directory: %s", m.Name, m.Source),
				Resource: m.Name,
				Detail: map[string]any{
					"source": m.Source,
					"path":   abs,
				},
			})
			continue
		}

		referencedModules[abs] = struct{}{}

		hasTF, err := hasTerraformFiles(abs)
		if err != nil {
			ctx.Findings = append(ctx.Findings, model.Finding{
				Severity: "warn",
				Code:     "MODULE_SOURCE_UNREADABLE",
				Message:  fmt.Sprintf("could not inspect module source directory for %q: %v", m.Name, err),
				Resource: m.Name,
				Detail: map[string]any{
					"source": m.Source,
					"path":   abs,
				},
			})
			continue
		}
		if !hasTF {
			ctx.Findings = append(ctx.Findings, model.Finding{
				Severity: "error",
				Code:     "MODULE_SOURCE_EMPTY",
				Message:  fmt.Sprintf("module %q source has no .tf files: %s", m.Name, m.Source),
				Resource: m.Name,
				Detail: map[string]any{
					"source": m.Source,
					"path":   abs,
				},
			})
		}
	}

	if modulesDir := filepath.Join(ctx.RootDir, "modules"); dirExists(modulesDir) {
		entries, err := os.ReadDir(modulesDir)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				modulePath := filepath.Join(modulesDir, e.Name())
				if _, ok := referencedModules[modulePath]; ok {
					continue
				}
				ctx.Findings = append(ctx.Findings, model.Finding{
					Severity: "warn",
					Code:     "MODULE_UNUSED_DIR",
					Message:  fmt.Sprintf("module directory %q exists but no module block imports it", modulePath),
					Detail: map[string]any{
						"path": modulePath,
					},
				})
			}
		}
	}
}

func hasTerraformFiles(path string) (bool, error) {
	found := false
	err := filepath.WalkDir(path, func(candidate string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".terraform" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(candidate, ".tf") {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	if err == nil || err == fs.SkipAll {
		return found, nil
	}
	return false, err
}

func dirExists(path string) bool {
	if stat, err := os.Stat(path); err == nil && stat.IsDir() {
		return true
	}
	return false
}

func parseResources(content *hcl.BodyContent, ctx *HCLContext) {
	blocks := content.Blocks.OfType("resource")
	for _, b := range blocks {
		if len(b.Labels) != 2 {
			continue
		}
		resourceType := b.Labels[0]
		resourceName := b.Labels[1]
		localAddr := resourceType + "." + resourceName
		addr := qualifyTerraformAddress(ctx.AddressPrefix, localAddr)

		cand := model.Candidate{
			Address:         addr,
			ResourceType:    resourceType,
			Mode:            "managed",
			Action:          "config",
			Source:          "hcl",
			SubscriptionID:  ctx.Subscription,
			RawRestrictions: map[string]any{},
		}
		ctx.CandidateMap[addr] = cand
		ctx.TraversalCandidates[localAddr] = cand
	}

	for pass := 0; pass < 3; pass++ {
		for _, b := range blocks {
			enrichResourceCandidate(b, ctx)
		}
	}
}

func enrichResourceCandidate(b *hcl.Block, ctx *HCLContext) {
	if len(b.Labels) != 2 {
		return
	}
	resourceType := b.Labels[0]
	resourceName := b.Labels[1]
	localAddr := resourceType + "." + resourceName
	addr := qualifyTerraformAddress(ctx.AddressPrefix, localAddr)

	cand, ok := ctx.CandidateMap[addr]
	if !ok {
		return
	}
	if cand.SubscriptionID == "" {
		cand.SubscriptionID = ctx.Subscription
	}
	cand.RawRestrictions = map[string]any{}

	blockContent, _, _ := b.Body.PartialContent(&hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{
			{Name: "location"},
			{Name: "name"},
			{Name: "profile_id"},
			{Name: "profile_name"},
			{Name: "resource_group_name"},
			{Name: "virtual_network_name"},
			{Name: "sku"},
		},
	})
	if blockContent != nil {
		if v, ok := blockContent.Attributes["location"]; ok {
			if val, ok := evalExpression(v.Expr, ctx); ok {
				if s, ok := toString(val); ok {
					cand.Location = s
				}
			}
		}
		if v, ok := blockContent.Attributes["name"]; ok {
			if val, ok := evalExpression(v.Expr, ctx); ok {
				if s, ok := toString(val); ok {
					cand.Name = s
				}
			}
		}
		if v, ok := blockContent.Attributes["resource_group_name"]; ok {
			if val, ok := evalExpression(v.Expr, ctx); ok {
				if s, ok := toString(val); ok {
					cand.ResourceGroup = s
				}
			}
		}
		profileID := ""
		if v, ok := blockContent.Attributes["profile_id"]; ok {
			if val, ok := evalExpression(v.Expr, ctx); ok {
				if s, ok := toString(val); ok {
					profileID = s
				}
			}
		}
		profileName := ""
		if v, ok := blockContent.Attributes["profile_name"]; ok {
			if val, ok := evalExpression(v.Expr, ctx); ok {
				if s, ok := toString(val); ok {
					profileName = s
				}
			}
		}
		mergeTrafficManagerProfileFields(&cand, profileName, profileID)
		if v, ok := blockContent.Attributes["virtual_network_name"]; ok {
			if val, ok := evalExpression(v.Expr, ctx); ok {
				if s, ok := toString(val); ok {
					cand.VirtualNetwork = s
				}
			}
		}
		if v, ok := blockContent.Attributes["sku"]; ok {
			if val, ok := evalExpression(v.Expr, ctx); ok {
				if s, ok := pickSKU(val); ok {
					cand.Sku = s
				}
			}
		}
	}

	parseRestrictionsFromBody(b.Body, cand.RawRestrictions, ctx)
	ctx.CandidateMap[addr] = cand
	ctx.TraversalCandidates[localAddr] = cand
}

func parseRestrictionsFromBody(body hcl.Body, store map[string]any, ctx *HCLContext) {
	restrictionSchema := &hcl.BodySchema{Blocks: []hcl.BlockHeaderSchema{{Type: "ip_restriction", LabelNames: nil}, {Type: "firewall_rule", LabelNames: nil}}}
	content, diags := body.Content(restrictionSchema)
	if diags.HasErrors() {
		return
	}
	for _, b := range content.Blocks {
		attrs, diags := b.Body.JustAttributes()
		if diags.HasErrors() {
			continue
		}
		entry := map[string]any{}
		for k, a := range attrs {
			if v, ok := evalExpression(a.Expr, ctx); ok {
				entry[k] = v
			}
		}
		if len(entry) > 0 {
			existing := store[b.Type]
			store[b.Type] = appendAsInterfaceSlice(existing, entry)
		}
	}
}

func appendAsInterfaceSlice(existing any, newEntry any) []any {
	if existing == nil {
		return []any{newEntry}
	}
	if list, ok := existing.([]any); ok {
		return append(list, newEntry)
	}
	return []any{existing, newEntry}
}

func attributeExpressions(attrs map[string]*hcl.Attribute) map[string]hcl.Expression {
	out := make(map[string]hcl.Expression, len(attrs))
	for name, attr := range attrs {
		out[name] = attr.Expr
	}
	return out
}

func qualifyTerraformAddress(prefix, addr string) string {
	if strings.TrimSpace(prefix) == "" {
		return addr
	}
	return prefix + "." + addr
}

func qualifyModulePrefix(prefix, moduleName, key string) string {
	moduleAddr := "module." + moduleName
	if strings.TrimSpace(key) != "" {
		moduleAddr += fmt.Sprintf("[%q]", key)
	}
	return qualifyTerraformAddress(prefix, moduleAddr)
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func loadAutoVariableFiles(root string, ctx *HCLContext) error {
	files := []string{}
	for _, candidate := range []string{
		filepath.Join(root, "terraform.tfvars"),
		filepath.Join(root, "terraform.tfvars.json"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			files = append(files, candidate)
		}
	}
	matches, err := filepath.Glob(filepath.Join(root, "*.auto.tfvars"))
	if err != nil {
		return err
	}
	files = append(files, matches...)

	for _, path := range files {
		if strings.HasSuffix(path, ".json") {
			continue
		}
		if err := loadVariableFile(path, ctx); err != nil {
			return err
		}
	}
	return nil
}

func loadVariableFile(path string, ctx *HCLContext) error {
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCLFile(path)
	if diags.HasErrors() {
		return diags
	}
	attrs, diags := file.Body.JustAttributes()
	if diags.HasErrors() {
		return diags
	}
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if value, ok := evalExpression(attrs[name].Expr, ctx); ok {
			ctx.Variables[name] = value
		}
	}
	return nil
}

func setContextSubscription(ctx *HCLContext, subscription string) {
	if ctx == nil {
		return
	}
	ctx.Subscription = strings.TrimSpace(subscription)
	if ctx.Subscription == "" {
		return
	}
	for address, candidate := range ctx.CandidateMap {
		if candidate.SubscriptionID != "" {
			continue
		}
		candidate.SubscriptionID = ctx.Subscription
		ctx.CandidateMap[address] = candidate
	}
	for address, candidate := range ctx.TraversalCandidates {
		if candidate.SubscriptionID != "" {
			continue
		}
		candidate.SubscriptionID = ctx.Subscription
		ctx.TraversalCandidates[address] = candidate
	}
}

func evalExpression(expr hcl.Expression, ctx *HCLContext) (any, bool) {
	switch v := expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		return ctyToGo(v.Val), true
	case *hclsyntax.TemplateExpr:
		parts := []string{}
		for _, p := range v.Parts {
			if lit, ok := p.(*hclsyntax.LiteralValueExpr); ok {
				if s, ok := ctyToString(lit.Val); ok {
					parts = append(parts, s)
				}
				continue
			}
			if tr, ok := p.(*hclsyntax.TemplateWrapExpr); ok {
				if val, ok := evalExpression(tr.Wrapped, ctx); ok {
					if s, ok := toString(val); ok {
						parts = append(parts, s)
					}
				}
				continue
			}
			if tr, ok := p.(*hclsyntax.ScopeTraversalExpr); ok {
				if val, ok := resolveTraversal(tr, ctx); ok {
					if s, ok := toString(val); ok {
						parts = append(parts, s)
					}
				}
				continue
			}
			if val, ok := evalExpression(p, ctx); ok {
				if s, ok := toString(val); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, ""), true
	case *hclsyntax.ScopeTraversalExpr:
		return resolveTraversal(v, ctx)
	case *hclsyntax.IndexExpr:
		return evalIndexExpression(v, ctx)
	case *hclsyntax.RelativeTraversalExpr:
		source, ok := evalExpression(v.Source, ctx)
		if !ok {
			return nil, false
		}
		return applyRelativeTraversal(source, v.Traversal)
	case *hclsyntax.FunctionCallExpr:
		return evalFunction(v, ctx)
	case *hclsyntax.ParenthesesExpr:
		return evalExpression(v.Expression, ctx)
	case *hclsyntax.BinaryOpExpr:
		left, lok := evalExpression(v.LHS, ctx)
		right, rok := evalExpression(v.RHS, ctx)
		if !lok || !rok {
			return nil, false
		}
		ls, loks := toString(left)
		rs, roks := toString(right)
		if loks && roks {
			return ls + rs, true
		}
		return nil, false
	case *hclsyntax.TupleConsExpr:
		items := make([]any, 0, len(v.Exprs))
		for _, e := range v.Exprs {
			if val, ok := evalExpression(e, ctx); ok {
				items = append(items, val)
			}
		}
		return items, true
	case *hclsyntax.ObjectConsExpr:
		obj := map[string]any{}
		for _, item := range v.Items {
			keyRaw, ok := evalExpression(item.KeyExpr, ctx)
			if !ok {
				continue
			}
			key, ok := toString(keyRaw)
			if !ok {
				continue
			}
			if val, ok := evalExpression(item.ValueExpr, ctx); ok {
				obj[key] = val
			}
		}
		return obj, true
	case *hclsyntax.ObjectConsKeyExpr:
		if tr, ok := v.Wrapped.(*hclsyntax.ScopeTraversalExpr); ok {
			split := tr.Traversal.SimpleSplit()
			if len(split.Abs) == 1 && len(split.Rel) == 0 {
				if root, ok := split.Abs[0].(hcl.TraverseRoot); ok {
					return root.Name, true
				}
			}
		}
		return evalExpression(v.Wrapped, ctx)
	}
	return nil, false
}

func evalFunction(fn *hclsyntax.FunctionCallExpr, ctx *HCLContext) (any, bool) {
	switch fn.Name {
	case "format":
		if len(fn.Args) == 0 {
			return nil, false
		}
		fmtRaw, ok := evalExpression(fn.Args[0], ctx)
		if !ok {
			return nil, false
		}
		pattern, ok := toString(fmtRaw)
		if !ok {
			return nil, false
		}
		parts := make([]any, 0, len(fn.Args)-1)
		for _, arg := range fn.Args[1:] {
			v, ok := evalExpression(arg, ctx)
			if !ok {
				return nil, false
			}
			parts = append(parts, fmtArg(v))
		}
		return fmt.Sprintf(pattern, parts...), true
	case "join":
		if len(fn.Args) != 2 {
			return nil, false
		}
		sepRaw, ok := evalExpression(fn.Args[0], ctx)
		if !ok {
			return nil, false
		}
		sep, ok := toString(sepRaw)
		if !ok {
			return nil, false
		}
		vals, ok := evalExpression(fn.Args[1], ctx)
		if !ok {
			return nil, false
		}
		list, ok := vals.([]any)
		if !ok {
			return nil, false
		}
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := toString(item); ok {
				out = append(out, s)
			}
		}
		return strings.Join(out, sep), true
	case "lower":
		if len(fn.Args) != 1 {
			return nil, false
		}
		v, ok := evalExpression(fn.Args[0], ctx)
		if !ok {
			return nil, false
		}
		s, ok := toString(v)
		if !ok {
			return nil, false
		}
		return strings.ToLower(s), true
	case "upper":
		if len(fn.Args) != 1 {
			return nil, false
		}
		v, ok := evalExpression(fn.Args[0], ctx)
		if !ok {
			return nil, false
		}
		s, ok := toString(v)
		if !ok {
			return nil, false
		}
		return strings.ToUpper(s), true
	}
	return nil, false
}

func fmtArg(v any) any {
	if s, ok := toString(v); ok {
		return s
	}
	return v
}

func resolveTraversal(expr *hclsyntax.ScopeTraversalExpr, ctx *HCLContext) (any, bool) {
	split := expr.Traversal.SimpleSplit()
	parts := []string{}
	if len(split.Abs) > 0 {
		for _, p := range split.Abs {
			switch step := p.(type) {
			case hcl.TraverseAttr:
				parts = append(parts, step.Name)
			case hcl.TraverseRoot:
				parts = append(parts, step.Name)
			}
		}
	}
	for _, p := range split.Rel {
		if step, ok := p.(hcl.TraverseAttr); ok {
			parts = append(parts, step.Name)
		}
	}
	if len(parts) < 2 {
		return nil, false
	}
	root := parts[0]
	key := parts[1]
	if ctx == nil {
		return nil, false
	}
	switch root {
	case "var":
		if val, ok := ctx.Variables[key]; ok {
			return traverseResolvedValue(val, parts[2:])
		}
	case "local":
		if val, ok := ctx.Locals[key]; ok {
			return traverseResolvedValue(val, parts[2:])
		}
	case "module":
		if val, ok := ctx.ModuleValues[key]; ok {
			return traverseResolvedValue(val, parts[2:])
		}
	case "each":
		switch key {
		case "key":
			return traverseResolvedValue(ctx.EachKey, parts[2:])
		case "value":
			return traverseResolvedValue(ctx.EachValue, parts[2:])
		}
	}
	return resolveResourceTraversal(parts, ctx)
}

func evalIndexExpression(expr *hclsyntax.IndexExpr, ctx *HCLContext) (any, bool) {
	collection, ok := evalExpression(expr.Collection, ctx)
	if !ok {
		return nil, false
	}
	keyValue, ok := evalExpression(expr.Key, ctx)
	if !ok {
		return nil, false
	}
	keyString, ok := toString(keyValue)
	if !ok {
		return nil, false
	}

	switch typed := collection.(type) {
	case map[string]any:
		value, ok := typed[keyString]
		return value, ok
	case []any:
		index, ok := toInt(keyValue)
		if !ok || index < 0 || index >= len(typed) {
			return nil, false
		}
		return typed[index], true
	default:
		return nil, false
	}
}

func applyRelativeTraversal(value any, traversal hcl.Traversal) (any, bool) {
	current := value
	for _, step := range traversal {
		switch typed := step.(type) {
		case hcl.TraverseAttr:
			next, ok := traverseResolvedValue(current, []string{typed.Name})
			if !ok {
				return nil, false
			}
			current = next
		case hcl.TraverseIndex:
			key, ok := toString(ctyToGo(typed.Key))
			if !ok {
				return nil, false
			}
			switch obj := current.(type) {
			case map[string]any:
				next, ok := obj[key]
				if !ok {
					return nil, false
				}
				current = next
			case []any:
				index, ok := toInt(ctyToGo(typed.Key))
				if !ok || index < 0 || index >= len(obj) {
					return nil, false
				}
				current = obj[index]
			default:
				return nil, false
			}
		default:
			return nil, false
		}
	}
	if current == nil {
		return nil, false
	}
	return current, true
}

func resolveResourceTraversal(parts []string, ctx *HCLContext) (any, bool) {
	if ctx == nil || len(parts) < 2 {
		return nil, false
	}
	candidate, ok := ctx.TraversalCandidates[parts[0]+"."+parts[1]]
	if !ok {
		return nil, false
	}
	if len(parts) == 2 {
		return candidate, true
	}
	return candidateTraversalValue(candidate, parts[2:])
}

func candidateTraversalValue(candidate model.Candidate, attrs []string) (any, bool) {
	if len(attrs) == 0 {
		return candidate, true
	}

	var value any
	switch attrs[0] {
	case "name":
		value = candidate.Name
	case "location":
		value = candidate.Location
	case "resource_group_name":
		value = candidate.ResourceGroup
	case "virtual_network_name":
		value = candidate.VirtualNetwork
	case "traffic_manager_profile", "profile_name":
		value = candidate.TrafficManagerProfile
	case "id":
		id, ok := buildCandidateResourceID(candidate)
		if !ok {
			return nil, false
		}
		value = id
	default:
		return nil, false
	}
	return traverseResolvedValue(value, attrs[1:])
}

func traverseResolvedValue(value any, attrs []string) (any, bool) {
	current := value
	if len(attrs) == 0 {
		if current == nil {
			return nil, false
		}
		return current, true
	}
	for _, attr := range attrs {
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[attr]
			if !ok {
				return nil, false
			}
			current = next
		default:
			return nil, false
		}
	}
	if current == nil {
		return nil, false
	}
	return current, true
}

func ctyToGo(v cty.Value) any {
	if !v.IsKnown() {
		return nil
	}
	if v.IsNull() {
		return nil
	}
	if v.Type() == cty.String {
		return v.AsString()
	}
	if v.Type() == cty.Number {
		if f, acc := v.AsBigFloat().Float64(); acc == big.Exact {
			return f
		}
		return nil
	}
	if v.Type() == cty.Bool {
		return v.True()
	}
	if v.Type().IsObjectType() {
		out := map[string]any{}
		for k := range v.Type().AttributeTypes() {
			out[k] = ctyToGo(v.GetAttr(k))
		}
		return out
	}
	if v.Type().IsTupleType() {
		items := v.AsValueSlice()
		vals := make([]any, 0, len(items))
		for _, it := range items {
			vals = append(vals, ctyToGo(it))
		}
		return vals
	}
	return nil
}

func ctyToString(v cty.Value) (string, bool) {
	if !v.IsKnown() || !v.Type().Equals(cty.String) || v.IsNull() {
		return "", false
	}
	return v.AsString(), true
}

func toString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case fmt.Stringer:
		return x.String(), true
	case int:
		return fmt.Sprintf("%d", x), true
	case int64:
		return fmt.Sprintf("%d", x), true
	case float64:
		return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%v", x), ".0"), "0."), true
	case bool:
		return fmt.Sprintf("%t", x), true
	}
	return "", false
}

func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	default:
		return 0, false
	}
}

func pickSKU(v any) (string, bool) {
	if s, ok := toString(v); ok {
		return s, true
	}
	if m, ok := v.(map[string]any); ok {
		for _, k := range []string{"name", "tier", "size"} {
			if val, ok := m[k]; ok {
				if s, ok := toString(val); ok {
					return s, true
				}
			}
		}
	}
	if list, ok := v.([]any); ok {
		for _, x := range list {
			if s, ok := pickSKU(x); ok {
				return s, true
			}
		}
	}
	return "", false
}
