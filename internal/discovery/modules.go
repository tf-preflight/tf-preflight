package discovery

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
)

type moduleInstance struct {
	AddressPrefix string
	Variables     map[string]any
}

func mergeLocalModuleCandidates(ctx *HCLContext) error {
	for _, call := range ctx.ModuleCalls {
		if call.ImportIndex < 0 || call.ImportIndex >= len(ctx.ModuleImports) {
			continue
		}
		mod := ctx.ModuleImports[call.ImportIndex]
		if mod.SourceKind != "local" || strings.TrimSpace(mod.ResolvedPath) == "" || !dirExists(mod.ResolvedPath) {
			continue
		}

		instances := expandModuleInstances(ctx, call)
		for _, instance := range instances {
			child, err := parseDirectory(mod.ResolvedPath, parseOptions{
				AddressPrefix: instance.AddressPrefix,
				SeedVariables: instance.Variables,
				Subscription:  ctx.Subscription,
			})
			if err != nil {
				return fmt.Errorf("failed parsing module %q: %w", mod.Name, err)
			}
			mergeChildContext(ctx, child)
		}
	}
	return nil
}

func expandModuleInstances(ctx *HCLContext, call moduleCall) []moduleInstance {
	forEachExpr, hasForEach := call.Attributes["for_each"]
	if !hasForEach {
		return []moduleInstance{{
			AddressPrefix: qualifyModulePrefix(ctx.AddressPrefix, call.Name, ""),
			Variables:     evaluateModuleInputs(ctx, call.Attributes),
		}}
	}

	forEachValue, ok := evalExpression(forEachExpr, ctx)
	if !ok {
		return nil
	}

	keysAndValues, ok := expandStaticForEach(forEachValue)
	if !ok {
		return nil
	}

	instances := make([]moduleInstance, 0, len(keysAndValues))
	for _, item := range keysAndValues {
		instanceCtx := *ctx
		instanceCtx.EachKey = item.key
		instanceCtx.EachValue = item.value
		instances = append(instances, moduleInstance{
			AddressPrefix: qualifyModulePrefix(ctx.AddressPrefix, call.Name, item.key),
			Variables:     evaluateModuleInputs(&instanceCtx, call.Attributes),
		})
	}
	return instances
}

type forEachKV struct {
	key   string
	value any
}

func expandStaticForEach(value any) ([]forEachKV, bool) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		items := make([]forEachKV, 0, len(keys))
		for _, key := range keys {
			items = append(items, forEachKV{key: key, value: typed[key]})
		}
		return items, true
	case []any:
		items := make([]forEachKV, 0, len(typed))
		for _, raw := range typed {
			key, ok := toString(raw)
			if !ok {
				return nil, false
			}
			items = append(items, forEachKV{key: key, value: raw})
		}
		return items, true
	default:
		return nil, false
	}
}

func evaluateModuleInputs(ctx *HCLContext, attrs map[string]hcl.Expression) map[string]any {
	inputs := map[string]any{}
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if isMetaModuleArgument(name) {
			continue
		}
		if value, ok := evalExpression(attrs[name], ctx); ok {
			inputs[name] = value
		}
	}
	return inputs
}

func isMetaModuleArgument(name string) bool {
	switch strings.TrimSpace(name) {
	case "source", "for_each", "count", "depends_on", "providers", "version":
		return true
	default:
		return false
	}
}

func mergeChildContext(parent, child *HCLContext) {
	if parent == nil || child == nil {
		return
	}
	for address, candidate := range child.CandidateMap {
		parent.CandidateMap[address] = candidate
	}
	parent.Findings = append(parent.Findings, child.Findings...)
	parent.Candidates = nil
}
