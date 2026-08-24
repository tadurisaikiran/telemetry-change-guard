package cloudformation

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type evaluation struct {
	value     ResolvedValue
	sensitive bool
}

type evaluationState struct {
	ctx               context.Context
	resolver          *Resolver
	steps             int
	issues            []ResolutionIssue
	conditionStack    map[string]struct{}
	issueLimitReached bool
}

func (state *evaluationState) evaluate(value any, valuePath string, depth int) (evaluation, error) {
	if err := state.ctx.Err(); err != nil {
		return evaluation{}, err
	}
	state.steps++
	if state.steps > state.resolver.limits.MaxSteps {
		return state.unknown("EVALUATION_LIMIT", valuePath, fmt.Sprintf("intrinsic evaluation exceeds the step limit of %d", state.resolver.limits.MaxSteps)), nil
	}
	if depth > state.resolver.limits.MaxDepth {
		return state.unknown("EVALUATION_LIMIT", valuePath, fmt.Sprintf("intrinsic evaluation exceeds the depth limit of %d", state.resolver.limits.MaxDepth)), nil
	}

	switch typed := value.(type) {
	case nil:
		return exact(ResolvedValue{Kind: ResolvedNull}), nil
	case string:
		return state.evaluateString(typed, valuePath), nil
	case json.Number:
		return exact(ResolvedValue{Kind: ResolvedNumber, Number: typed.String()}), nil
	case bool:
		return exact(ResolvedValue{Kind: ResolvedBoolean, Boolean: typed}), nil
	case []any:
		return state.evaluateList(typed, valuePath, depth)
	case map[string]any:
		return state.evaluateObject(typed, valuePath, depth)
	default:
		return state.unknown("INVALID_VALUE", valuePath, fmt.Sprintf("unsupported decoded JSON value type %T", value)), nil
	}
}

func (state *evaluationState) evaluateString(value, valuePath string) evaluation {
	first := strings.Index(value, "{{resolve:")
	if first < 0 {
		if !state.stringWithinLimit(valuePath, len(value)) {
			return state.unknown("EVALUATION_LIMIT", valuePath, "literal string exceeds the expansion limit")
		}
		return exact(stringValue(value))
	}

	fragments := make([]ResolvedFragment, 0, 3)
	remaining := value
	knownBytes := 0
	for {
		start := strings.Index(remaining, "{{resolve:")
		if start < 0 {
			if remaining != "" {
				fragments = appendKnownFragment(fragments, remaining)
				knownBytes += len(remaining)
			}
			break
		}
		if start > 0 {
			known := remaining[:start]
			fragments = appendKnownFragment(fragments, known)
			knownBytes += len(known)
		}
		endRelative := strings.Index(remaining[start+2:], "}}")
		if endRelative < 0 {
			state.addIssue("INVALID_DYNAMIC_REFERENCE", valuePath, "dynamic reference is missing its closing braces")
			return state.unknownWithoutIssue()
		}
		end := start + 2 + endRelative + 2
		expression := remaining[start:end]
		fragments = append(fragments, ResolvedFragment{Expression: marshalExpression(expression)})
		state.addIssue("DYNAMIC_REFERENCE", valuePath, "dynamic reference requires deploy-time external resolution")
		remaining = remaining[end:]
	}
	if !state.stringWithinLimit(valuePath, knownBytes) {
		return state.unknown("EVALUATION_LIMIT", valuePath, "known dynamic-reference fragments exceed the expansion limit")
	}
	resolutionState := ResolutionPartial
	if knownBytes == 0 && len(fragments) == 1 {
		resolutionState = ResolutionUnknown
	}
	return evaluation{
		value: ResolvedValue{
			State:     resolutionState,
			Kind:      ResolvedString,
			Fragments: fragments,
		},
		sensitive: true,
	}
}

func (state *evaluationState) evaluateList(values []any, valuePath string, depth int) (evaluation, error) {
	resolved := make([]ResolvedValue, len(values))
	resultState := ResolutionExact
	sensitive := false
	for index, item := range values {
		child, err := state.evaluate(item, fmt.Sprintf("%s/%d", valuePath, index), depth+1)
		if err != nil {
			return evaluation{}, err
		}
		resolved[index] = child.value
		if child.value.State != ResolutionExact {
			resultState = ResolutionPartial
		}
		sensitive = sensitive || child.sensitive
	}
	return evaluation{
		value:     ResolvedValue{State: resultState, Kind: ResolvedList, List: resolved},
		sensitive: sensitive,
	}, nil
}

func (state *evaluationState) evaluateObject(values map[string]any, valuePath string, depth int) (evaluation, error) {
	intrinsicKeys := make([]string, 0, 1)
	for key := range values {
		if key == "Ref" || strings.HasPrefix(key, "Fn::") {
			intrinsicKeys = append(intrinsicKeys, key)
		}
	}
	if len(intrinsicKeys) != 0 {
		sort.Strings(intrinsicKeys)
		if len(values) != 1 || len(intrinsicKeys) != 1 {
			return state.unknown("INVALID_INTRINSIC", valuePath, fmt.Sprintf("intrinsic key %q must be the only key in its object", intrinsicKeys[0])), nil
		}
		key := intrinsicKeys[0]
		argument := values[key]
		intrinsicPath := valuePath + "/" + escapeJSONPointer(key)
		switch key {
		case "Ref":
			return state.evaluateRef(argument, intrinsicPath)
		case "Fn::Sub":
			return state.evaluateSub(argument, intrinsicPath, depth+1)
		case "Fn::Join":
			return state.evaluateJoin(argument, intrinsicPath, depth+1)
		case "Fn::GetAtt":
			return state.evaluateGetAtt(argument, intrinsicPath)
		case "Fn::If":
			return state.evaluateIf(argument, intrinsicPath, depth+1)
		case "Fn::FindInMap":
			return state.evaluateFindInMap(argument, intrinsicPath, depth+1)
		default:
			return state.unknown("UNSUPPORTED_INTRINSIC", intrinsicPath, fmt.Sprintf("intrinsic %s is not in the supported evaluation subset", key)), nil
		}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fields := make([]ResolvedField, 0, len(keys))
	resultState := ResolutionExact
	sensitive := false
	for _, key := range keys {
		child, err := state.evaluate(values[key], valuePath+"/"+escapeJSONPointer(key), depth+1)
		if err != nil {
			return evaluation{}, err
		}
		fields = append(fields, ResolvedField{Name: key, Value: child.value})
		if child.value.State != ResolutionExact {
			resultState = ResolutionPartial
		}
		sensitive = sensitive || child.sensitive
	}
	return evaluation{
		value:     ResolvedValue{State: resultState, Kind: ResolvedObject, Object: fields},
		sensitive: sensitive,
	}, nil
}

func (state *evaluationState) evaluateRef(argument any, valuePath string) (evaluation, error) {
	name, ok := argument.(string)
	if !ok || name == "" || name != strings.TrimSpace(name) {
		return state.unknown("INVALID_REF", valuePath, "Ref requires a non-empty literal logical name"), nil
	}
	if strings.HasPrefix(name, "AWS::") {
		value, exists := state.resolver.pseudoParameters[name]
		if !exists {
			return state.unknown("UNRESOLVED_PSEUDO_PARAMETER", valuePath, fmt.Sprintf("pseudo parameter %q has no explicit resolution evidence", name)), nil
		}
		return exact(value), nil
	}
	if spec, exists := state.resolver.parameters[name]; exists {
		if value, supplied := state.resolver.parameterValues[name]; supplied {
			return evaluation{value: value, sensitive: spec.sensitive}, nil
		}
		if spec.defaultValue != nil {
			return evaluation{value: *spec.defaultValue, sensitive: spec.sensitive}, nil
		}
		if spec.dynamic {
			return state.unknownSensitive("UNRESOLVED_DYNAMIC_PARAMETER", valuePath, fmt.Sprintf("parameter %q requires external SSM resolution", name), spec.sensitive), nil
		}
		return state.unknownSensitive("UNRESOLVED_PARAMETER", valuePath, fmt.Sprintf("parameter %q has no supplied value or default", name), spec.sensitive), nil
	}
	if _, exists := state.resolver.resources[name]; exists {
		if value, supplied := state.resolver.resourceRefs[name]; supplied {
			return exact(value), nil
		}
		return state.unknown("UNRESOLVED_RESOURCE_REF", valuePath, fmt.Sprintf("resource %q Ref value requires authoritative runtime evidence", name)), nil
	}
	return state.unknown("UNKNOWN_REFERENCE", valuePath, fmt.Sprintf("Ref target %q is not a declared parameter, resource, or supported pseudo parameter", name)), nil
}

func (state *evaluationState) evaluateGetAtt(argument any, valuePath string) (evaluation, error) {
	arguments, ok := argument.([]any)
	if !ok || len(arguments) != 2 {
		return state.unknown("INVALID_GETATT", valuePath, "Fn::GetAtt requires [resource logical ID, attribute name]"), nil
	}
	logicalID, resourceOK := arguments[0].(string)
	attribute, attributeOK := arguments[1].(string)
	if !resourceOK || !attributeOK || logicalID == "" || attribute == "" ||
		logicalID != strings.TrimSpace(logicalID) || attribute != strings.TrimSpace(attribute) {
		return state.unknown("INVALID_GETATT", valuePath, "Fn::GetAtt resource and attribute must be non-empty literal strings"), nil
	}
	if _, exists := state.resolver.resources[logicalID]; !exists {
		return state.unknown("UNKNOWN_RESOURCE", valuePath, fmt.Sprintf("Fn::GetAtt resource %q is not declared by the template", logicalID)), nil
	}
	if attributes, exists := state.resolver.resourceAttributes[logicalID]; exists {
		if value, supplied := attributes[attribute]; supplied {
			return exact(value), nil
		}
	}
	return state.unknown("UNRESOLVED_RESOURCE_ATTRIBUTE", valuePath, fmt.Sprintf("resource %q attribute %q requires authoritative runtime evidence", logicalID, attribute)), nil
}

func (state *evaluationState) evaluateFindInMap(argument any, valuePath string, depth int) (evaluation, error) {
	arguments, ok := argument.([]any)
	if !ok || len(arguments) != 3 {
		return state.unknown("INVALID_FIND_IN_MAP", valuePath, "Fn::FindInMap requires [map name, top-level key, second-level key]"), nil
	}
	keys := make([]string, 3)
	sensitive := false
	for index, raw := range arguments {
		resolved, err := state.evaluate(raw, fmt.Sprintf("%s/%d", valuePath, index), depth+1)
		if err != nil {
			return evaluation{}, err
		}
		sensitive = sensitive || resolved.sensitive
		key, exactString := exactScalarString(resolved.value)
		if resolved.value.State != ResolutionExact || !exactString {
			state.addIssue("UNRESOLVED_MAPPING_KEY", fmt.Sprintf("%s/%d", valuePath, index), "Fn::FindInMap keys must resolve to exact strings")
			return evaluation{value: unknownValue(), sensitive: sensitive}, nil
		}
		keys[index] = key
	}
	mappingRaw, exists := state.resolver.mappings[keys[0]]
	if !exists {
		return state.unknownSensitive("MAPPING_NOT_FOUND", valuePath, fmt.Sprintf("mapping %q is not declared", keys[0]), sensitive), nil
	}
	top, ok := mappingRaw.(map[string]any)
	if !ok {
		return state.unknownSensitive("INVALID_MAPPING", valuePath, fmt.Sprintf("mapping %q is not an object", keys[0]), sensitive), nil
	}
	secondRaw, exists := top[keys[1]]
	if !exists {
		return state.unknownSensitive("MAPPING_KEY_NOT_FOUND", valuePath, fmt.Sprintf("mapping %q has no top-level key %q", keys[0], keys[1]), sensitive), nil
	}
	second, ok := secondRaw.(map[string]any)
	if !ok {
		return state.unknownSensitive("INVALID_MAPPING", valuePath, fmt.Sprintf("mapping %q top-level key %q is not an object", keys[0], keys[1]), sensitive), nil
	}
	resultRaw, exists := second[keys[2]]
	if !exists {
		return state.unknownSensitive("MAPPING_KEY_NOT_FOUND", valuePath, fmt.Sprintf("mapping %q path %q/%q does not exist", keys[0], keys[1], keys[2]), sensitive), nil
	}
	result, err := state.evaluate(resultRaw, valuePath+"/result", depth+1)
	if err != nil {
		return evaluation{}, err
	}
	result.sensitive = result.sensitive || sensitive
	return result, nil
}

func (state *evaluationState) evaluateJoin(argument any, valuePath string, depth int) (evaluation, error) {
	arguments, ok := argument.([]any)
	if !ok || len(arguments) != 2 {
		return state.unknown("INVALID_JOIN", valuePath, "Fn::Join requires [literal delimiter, list of values]"), nil
	}
	delimiter, ok := arguments[0].(string)
	if !ok {
		return state.unknown("INVALID_JOIN", valuePath+"/0", "Fn::Join delimiter must be a literal string"), nil
	}
	delimiterResolution := state.evaluateString(delimiter, valuePath+"/0")
	if delimiterResolution.value.State != ResolutionExact {
		return state.unknownSensitive("UNRESOLVED_JOIN_DELIMITER", valuePath+"/0", "Fn::Join delimiter is not exact", delimiterResolution.sensitive), nil
	}
	list, err := state.evaluate(arguments[1], valuePath+"/1", depth+1)
	if err != nil {
		return evaluation{}, err
	}
	if list.value.Kind != ResolvedList {
		state.addIssue("INVALID_JOIN", valuePath+"/1", "Fn::Join values must resolve to a list")
		return evaluation{value: unknownValue(), sensitive: list.sensitive}, nil
	}
	if list.value.State == ResolutionUnknown {
		return evaluation{value: unknownValue(), sensitive: list.sensitive}, nil
	}

	fragments := make([]ResolvedFragment, 0, len(list.value.List)*2)
	var builder strings.Builder
	partial := list.value.State != ResolutionExact
	elementCount := 0
	for index, item := range list.value.List {
		if item.Kind == ResolvedNoValue {
			continue
		}
		if elementCount > 0 {
			builder.WriteString(delimiter)
			fragments = appendKnownFragment(fragments, delimiter)
		}
		elementCount++
		if item.State == ResolutionExact {
			text, scalar := exactScalarString(item)
			if !scalar {
				state.addIssue("INVALID_JOIN_VALUE", fmt.Sprintf("%s/1/%d", valuePath, index), "Fn::Join elements must resolve to strings or numbers")
				return evaluation{value: unknownValue(), sensitive: list.sensitive}, nil
			}
			builder.WriteString(text)
			fragments = appendKnownFragment(fragments, text)
			continue
		}
		partial = true
		if item.Kind == ResolvedString && len(item.Fragments) != 0 {
			fragments = append(fragments, item.Fragments...)
			for _, fragment := range item.Fragments {
				if fragment.Known {
					builder.WriteString(fragment.Text)
				}
			}
		} else {
			fragments = append(fragments, ResolvedFragment{Expression: marshalExpression(arguments[1])})
		}
	}
	if !state.stringWithinLimit(valuePath, builder.Len()) {
		return state.unknownSensitive("EVALUATION_LIMIT", valuePath, "Fn::Join output exceeds the string limit", list.sensitive), nil
	}
	if !partial {
		return evaluation{value: stringValue(builder.String()), sensitive: list.sensitive}, nil
	}
	return evaluation{
		value:     ResolvedValue{State: ResolutionPartial, Kind: ResolvedString, Fragments: fragments},
		sensitive: list.sensitive,
	}, nil
}

func (state *evaluationState) evaluateIf(argument any, valuePath string, depth int) (evaluation, error) {
	arguments, ok := argument.([]any)
	if !ok || len(arguments) != 3 {
		return state.unknown("INVALID_IF", valuePath, "Fn::If requires [condition name, true value, false value]"), nil
	}
	conditionName, ok := arguments[0].(string)
	if !ok || conditionName == "" || conditionName != strings.TrimSpace(conditionName) {
		return state.unknown("INVALID_IF", valuePath+"/0", "Fn::If condition name must be a non-empty literal string"), nil
	}
	condition, err := state.evaluateCondition(conditionName, valuePath+"/0", depth+1)
	if err != nil {
		return evaluation{}, err
	}
	if condition.known {
		branch := 2
		if condition.value {
			branch = 1
		}
		return state.evaluate(arguments[branch], fmt.Sprintf("%s/%d", valuePath, branch), depth+1)
	}

	trueValue, err := state.evaluate(arguments[1], valuePath+"/1", depth+1)
	if err != nil {
		return evaluation{}, err
	}
	falseValue, err := state.evaluate(arguments[2], valuePath+"/2", depth+1)
	if err != nil {
		return evaluation{}, err
	}
	sensitive := trueValue.sensitive || falseValue.sensitive
	if trueValue.value.State == ResolutionExact && falseValue.value.State == ResolutionExact &&
		reflect.DeepEqual(trueValue.value, falseValue.value) {
		return evaluation{value: trueValue.value, sensitive: sensitive}, nil
	}
	resultState := ResolutionPartial
	if trueValue.value.State == ResolutionUnknown && falseValue.value.State == ResolutionUnknown {
		resultState = ResolutionUnknown
	}
	return evaluation{
		value: ResolvedValue{
			State:        resultState,
			Kind:         ResolvedChoice,
			Alternatives: []ResolvedValue{trueValue.value, falseValue.value},
		},
		sensitive: sensitive,
	}, nil
}

func (state *evaluationState) evaluateSub(argument any, valuePath string, depth int) (evaluation, error) {
	template, variables, ok := parseSubArgument(argument)
	if !ok {
		return state.unknown("INVALID_SUB", valuePath, "Fn::Sub requires a string or [string, variable map]"), nil
	}
	fragments := make([]ResolvedFragment, 0, 4)
	var builder strings.Builder
	partial := false
	sensitive := false
	remaining := template
	for {
		start := strings.Index(remaining, "${")
		if start < 0 {
			builder.WriteString(remaining)
			fragments = appendKnownFragment(fragments, remaining)
			break
		}
		literal := remaining[:start]
		builder.WriteString(literal)
		fragments = appendKnownFragment(fragments, literal)
		endRelative := strings.IndexByte(remaining[start+2:], '}')
		if endRelative < 0 {
			return state.unknownSensitive("INVALID_SUB", valuePath, "Fn::Sub variable is missing its closing brace", sensitive), nil
		}
		end := start + 2 + endRelative
		variable := remaining[start+2 : end]
		remaining = remaining[end+1:]
		if strings.HasPrefix(variable, "!") {
			escaped := "${" + strings.TrimPrefix(variable, "!") + "}"
			builder.WriteString(escaped)
			fragments = appendKnownFragment(fragments, escaped)
			continue
		}
		if variable == "" {
			return state.unknownSensitive("INVALID_SUB", valuePath, "Fn::Sub variable name must not be empty", sensitive), nil
		}
		resolved, expression, err := state.resolveSubVariable(variable, variables, valuePath, depth+1)
		if err != nil {
			return evaluation{}, err
		}
		sensitive = sensitive || resolved.sensitive
		if resolved.value.State == ResolutionExact {
			text, scalar := exactScalarString(resolved.value)
			if !scalar {
				return state.unknownSensitive("INVALID_SUB_VALUE", valuePath, fmt.Sprintf("Fn::Sub variable %q must resolve to a string or number", variable), sensitive), nil
			}
			builder.WriteString(text)
			fragments = appendKnownFragment(fragments, text)
			continue
		}
		partial = true
		if resolved.value.Kind == ResolvedString && len(resolved.value.Fragments) != 0 {
			fragments = append(fragments, resolved.value.Fragments...)
			for _, fragment := range resolved.value.Fragments {
				if fragment.Known {
					builder.WriteString(fragment.Text)
				}
			}
		} else {
			fragments = append(fragments, ResolvedFragment{Expression: expression})
		}
	}
	if !state.stringWithinLimit(valuePath, builder.Len()) {
		return state.unknownSensitive("EVALUATION_LIMIT", valuePath, "Fn::Sub output exceeds the string limit", sensitive), nil
	}
	if !partial {
		return evaluation{value: stringValue(builder.String()), sensitive: sensitive}, nil
	}
	return evaluation{
		value:     ResolvedValue{State: ResolutionPartial, Kind: ResolvedString, Fragments: fragments},
		sensitive: sensitive,
	}, nil
}

func parseSubArgument(argument any) (string, map[string]any, bool) {
	if template, ok := argument.(string); ok {
		return template, nil, true
	}
	arguments, ok := argument.([]any)
	if !ok || len(arguments) != 2 {
		return "", nil, false
	}
	template, ok := arguments[0].(string)
	if !ok {
		return "", nil, false
	}
	variables, ok := arguments[1].(map[string]any)
	if !ok {
		return "", nil, false
	}
	return template, variables, true
}

func (state *evaluationState) resolveSubVariable(
	name string,
	variables map[string]any,
	valuePath string,
	depth int,
) (evaluation, json.RawMessage, error) {
	if raw, exists := variables[name]; exists {
		expression := marshalExpression(raw)
		resolved, err := state.evaluate(raw, valuePath+"/variables/"+escapeJSONPointer(name), depth+1)
		return resolved, expression, err
	}
	if logicalID, attribute, hasAttribute := strings.Cut(name, "."); hasAttribute {
		expressionValue := map[string]any{"Fn::GetAtt": []any{logicalID, attribute}}
		resolved, err := state.evaluateGetAtt(expressionValue["Fn::GetAtt"], valuePath+"/variables/"+escapeJSONPointer(name))
		return resolved, marshalExpression(expressionValue), err
	}
	expressionValue := map[string]any{"Ref": name}
	resolved, err := state.evaluateRef(name, valuePath+"/variables/"+escapeJSONPointer(name))
	return resolved, marshalExpression(expressionValue), err
}

func exact(value ResolvedValue) evaluation {
	value.State = ResolutionExact
	return evaluation{value: value}
}

func unknownValue() ResolvedValue {
	return ResolvedValue{State: ResolutionUnknown, Kind: ResolvedUnknown}
}

func (state *evaluationState) unknown(code, valuePath, message string) evaluation {
	state.addIssue(code, valuePath, message)
	return state.unknownWithoutIssue()
}

func (state *evaluationState) unknownSensitive(code, valuePath, message string, sensitive bool) evaluation {
	result := state.unknown(code, valuePath, message)
	result.sensitive = sensitive
	return result
}

func (state *evaluationState) unknownWithoutIssue() evaluation {
	return evaluation{value: unknownValue()}
}

func (state *evaluationState) addIssue(code, valuePath, message string) {
	if len(state.issues) < state.resolver.limits.MaxIssues {
		state.issues = append(state.issues, ResolutionIssue{Code: code, Path: valuePath, Message: message})
		return
	}
	state.issueLimitReached = true
}

func (state *evaluationState) stringWithinLimit(valuePath string, size int) bool {
	if size <= state.resolver.limits.MaxStringBytes {
		return true
	}
	state.addIssue("EVALUATION_LIMIT", valuePath, fmt.Sprintf("resolved string exceeds the %d-byte limit", state.resolver.limits.MaxStringBytes))
	return false
}

func exactScalarString(value ResolvedValue) (string, bool) {
	if value.State != ResolutionExact {
		return "", false
	}
	switch value.Kind {
	case ResolvedString:
		return value.String, true
	case ResolvedNumber:
		return value.Number, true
	default:
		return "", false
	}
}

func appendKnownFragment(fragments []ResolvedFragment, text string) []ResolvedFragment {
	if text == "" {
		return fragments
	}
	if len(fragments) != 0 && fragments[len(fragments)-1].Known {
		fragments[len(fragments)-1].Text += text
		return fragments
	}
	return append(fragments, ResolvedFragment{Known: true, Text: text})
}

func escapeJSONPointer(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}
