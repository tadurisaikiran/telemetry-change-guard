package cloudformation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ResolutionState is the fail-closed confidence of an evaluated value.
type ResolutionState string

const (
	ResolutionExact   ResolutionState = "EXACT"
	ResolutionPartial ResolutionState = "PARTIAL"
	ResolutionUnknown ResolutionState = "UNKNOWN"
)

// ResolvedKind identifies the structural type retained by a resolution.
type ResolvedKind string

const (
	ResolvedUnknown ResolvedKind = "unknown"
	ResolvedString  ResolvedKind = "string"
	ResolvedNumber  ResolvedKind = "number"
	ResolvedBoolean ResolvedKind = "boolean"
	ResolvedNull    ResolvedKind = "null"
	ResolvedList    ResolvedKind = "list"
	ResolvedObject  ResolvedKind = "object"
	ResolvedChoice  ResolvedKind = "choice"
	ResolvedNoValue ResolvedKind = "no_value"
)

// ResolvedValue is a deterministic, typed representation of a literal,
// partially known structure, or conditional choice. String is populated only
// for an exact string. Fragments retain known portions of a partial string.
type ResolvedValue struct {
	State        ResolutionState    `json:"state"`
	Kind         ResolvedKind       `json:"kind"`
	String       string             `json:"string,omitempty"`
	Number       string             `json:"number,omitempty"`
	Boolean      bool               `json:"boolean,omitempty"`
	List         []ResolvedValue    `json:"list,omitempty"`
	Object       []ResolvedField    `json:"object,omitempty"`
	Fragments    []ResolvedFragment `json:"fragments,omitempty"`
	Alternatives []ResolvedValue    `json:"alternatives,omitempty"`
}

// ResolvedField is one field in a stable, name-sorted resolved object.
type ResolvedField struct {
	Name  string        `json:"name"`
	Value ResolvedValue `json:"value"`
}

// ResolvedFragment preserves a known or unresolved part of Fn::Sub or
// Fn::Join output without inventing a replacement value.
type ResolvedFragment struct {
	Known      bool            `json:"known"`
	Text       string          `json:"text,omitempty"`
	Expression json.RawMessage `json:"expression,omitempty"`
}

// ResolutionIssue explains why a value is not exact. Code and Path are stable
// machine fields; Message is intended for operators.
type ResolutionIssue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Resolution preserves the original unresolved expression and resource
// provenance even when exact evaluation is impossible.
type Resolution struct {
	State      ResolutionState   `json:"state"`
	Value      ResolvedValue     `json:"value"`
	Expression json.RawMessage   `json:"expression"`
	Provenance Provenance        `json:"provenance"`
	Sensitive  bool              `json:"sensitive,omitempty"`
	Issues     []ResolutionIssue `json:"issues,omitempty"`
}

// ExactString returns the exact string value when the resolution has one.
func (resolution Resolution) ExactString() (string, bool) {
	if resolution.State != ResolutionExact || resolution.Value.Kind != ResolvedString {
		return "", false
	}
	return resolution.Value.String, true
}

// ResolutionLimits bounds recursive and expanding intrinsic evaluation. Zero
// fields use production defaults; negative values are invalid.
type ResolutionLimits struct {
	MaxDepth             int
	MaxSteps             int
	MaxIssues            int
	MaxStringBytes       int
	MaxContextValueBytes int64
}

// DefaultResolutionLimits returns the default intrinsic evaluation budgets.
func DefaultResolutionLimits() ResolutionLimits {
	return ResolutionLimits{
		MaxDepth:             64,
		MaxSteps:             10_000,
		MaxIssues:            256,
		MaxStringBytes:       1 << 20,
		MaxContextValueBytes: 1 << 20,
	}
}

// ResolutionInputs contains only explicit deploy-time evidence. Parameter
// values are already-resolved values, not SSM parameter keys. Resource Ref and
// attribute values must likewise come from an authoritative external source;
// the resolver never queries AWS.
type ResolutionInputs struct {
	AccountID          string
	Region             string
	Partition          string
	URLSuffix          string
	StackID            string
	StackName          string
	NotificationARNs   []string
	ParameterValues    map[string]json.RawMessage
	ConditionValues    map[string]bool
	ResourceRefs       map[string]json.RawMessage
	ResourceAttributes map[string]map[string]json.RawMessage
}

type parameterSpec struct {
	typeName     string
	defaultValue *ResolvedValue
	sensitive    bool
	dynamic      bool
}

// Resolver evaluates intrinsic expressions in one synthesized stack.
type Resolver struct {
	stack              Stack
	limits             ResolutionLimits
	parameters         map[string]parameterSpec
	parameterValues    map[string]ResolvedValue
	conditions         map[string]any
	conditionValues    map[string]bool
	mappings           map[string]any
	resources          map[string]struct{}
	resourceRefs       map[string]ResolvedValue
	resourceAttributes map[string]map[string]ResolvedValue
	pseudoParameters   map[string]ResolvedValue
}

var (
	partitionPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	urlSuffixPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)
)

// NewResolver validates and freezes the explicit evaluation context for one
// stack. Stack environment and explicit account/Region evidence must agree.
func NewResolver(stack Stack, inputs ResolutionInputs, limits ResolutionLimits) (*Resolver, error) {
	normalized, err := normalizeResolutionLimits(limits)
	if err != nil {
		return nil, err
	}
	resolver := &Resolver{
		stack:              stack,
		limits:             normalized,
		parameters:         make(map[string]parameterSpec),
		parameterValues:    make(map[string]ResolvedValue),
		conditions:         make(map[string]any),
		conditionValues:    make(map[string]bool),
		mappings:           make(map[string]any),
		resources:          make(map[string]struct{}),
		resourceRefs:       make(map[string]ResolvedValue),
		resourceAttributes: make(map[string]map[string]ResolvedValue),
		pseudoParameters:   make(map[string]ResolvedValue),
	}
	for _, resource := range stack.Template.Resources {
		if _, duplicate := resolver.resources[resource.LogicalID]; duplicate {
			return nil, fmt.Errorf("stack %q contains duplicate resource %q", stack.ID, resource.LogicalID)
		}
		resolver.resources[resource.LogicalID] = struct{}{}
	}
	if err := resolver.loadTemplateSections(); err != nil {
		return nil, err
	}
	if err := resolver.loadInputs(inputs); err != nil {
		return nil, err
	}
	return resolver, nil
}

// Resolve evaluates one JSON value without executing code, transforms, or
// network requests. Unsupported and unresolved expressions return a
// fail-closed state; context cancellation is returned as an operational error.
func (resolver *Resolver) Resolve(ctx context.Context, expression json.RawMessage, provenance Provenance) (Resolution, error) {
	if resolver == nil {
		return Resolution{}, errors.New("CloudFormation resolver is nil")
	}
	if err := ctx.Err(); err != nil {
		return Resolution{}, err
	}
	value, err := decodeResolutionJSON(expression, resolver.limits)
	if err != nil {
		return Resolution{}, fmt.Errorf("decode intrinsic expression: %w", err)
	}
	state := evaluationState{
		ctx:            ctx,
		resolver:       resolver,
		conditionStack: make(map[string]struct{}),
	}
	evaluated, err := state.evaluate(value, "$", 1)
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{
		State:      evaluated.value.State,
		Value:      evaluated.value,
		Expression: cloneRaw(expression),
		Provenance: provenance,
		Sensitive:  evaluated.sensitive,
		Issues:     append([]ResolutionIssue(nil), state.issues...),
	}, nil
}

func (resolver *Resolver) loadTemplateSections() error {
	for _, named := range resolver.stack.Template.Parameters {
		definition, err := decodeResolutionJSON(named.Value, resolver.limits)
		if err != nil {
			return fmt.Errorf("parameter %q: %w", named.Name, err)
		}
		fields, ok := definition.(map[string]any)
		if !ok {
			return fmt.Errorf("parameter %q definition must be an object", named.Name)
		}
		typeName, ok := fields["Type"].(string)
		if !ok || strings.TrimSpace(typeName) == "" || typeName != strings.TrimSpace(typeName) {
			return fmt.Errorf("parameter %q Type must be a non-empty string without surrounding whitespace", named.Name)
		}
		spec := parameterSpec{typeName: typeName, dynamic: strings.HasPrefix(typeName, "AWS::SSM::Parameter::Value<")}
		if rawNoEcho, exists := fields["NoEcho"]; exists {
			noEcho, err := parseNoEcho(rawNoEcho)
			if err != nil {
				return fmt.Errorf("parameter %q NoEcho: %w", named.Name, err)
			}
			spec.sensitive = noEcho
		}
		if rawDefault, exists := fields["Default"]; exists && !spec.dynamic {
			resolved, err := normalizeParameterValue(typeName, rawDefault)
			if err != nil {
				return fmt.Errorf("parameter %q Default: %w", named.Name, err)
			}
			spec.defaultValue = &resolved
		}
		resolver.parameters[named.Name] = spec
	}
	for _, named := range resolver.stack.Template.Conditions {
		value, err := decodeResolutionJSON(named.Value, resolver.limits)
		if err != nil {
			return fmt.Errorf("condition %q: %w", named.Name, err)
		}
		resolver.conditions[named.Name] = value
	}
	for _, named := range resolver.stack.Template.Mappings {
		value, err := decodeResolutionJSON(named.Value, resolver.limits)
		if err != nil {
			return fmt.Errorf("mapping %q: %w", named.Name, err)
		}
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("mapping %q must be an object", named.Name)
		}
		resolver.mappings[named.Name] = value
	}
	return nil
}

func (resolver *Resolver) loadInputs(inputs ResolutionInputs) error {
	account, err := reconcileAddress("account", resolver.stack.Environment.Account, "unknown-account", inputs.AccountID, accountPattern)
	if err != nil {
		return err
	}
	region, err := reconcileAddress("Region", resolver.stack.Environment.Region, "unknown-region", inputs.Region, regionPattern)
	if err != nil {
		return err
	}
	if account != "" {
		resolver.pseudoParameters["AWS::AccountId"] = stringValue(account)
	}
	if region != "" {
		resolver.pseudoParameters["AWS::Region"] = stringValue(region)
	}
	if inputs.Partition != "" {
		if inputs.Partition != strings.TrimSpace(inputs.Partition) || !partitionPattern.MatchString(inputs.Partition) {
			return fmt.Errorf("partition %q is invalid", inputs.Partition)
		}
		resolver.pseudoParameters["AWS::Partition"] = stringValue(inputs.Partition)
	}
	if inputs.URLSuffix != "" {
		if inputs.URLSuffix != strings.TrimSpace(inputs.URLSuffix) || !urlSuffixPattern.MatchString(inputs.URLSuffix) {
			return fmt.Errorf("URL suffix %q is invalid", inputs.URLSuffix)
		}
		resolver.pseudoParameters["AWS::URLSuffix"] = stringValue(inputs.URLSuffix)
	}
	if inputs.StackID != "" {
		if inputs.StackID != strings.TrimSpace(inputs.StackID) {
			return errors.New("stack ID must not have surrounding whitespace")
		}
		resolver.pseudoParameters["AWS::StackId"] = stringValue(inputs.StackID)
	}
	stackName, err := reconcileLiteral("stack name", resolver.stack.StackName, inputs.StackName)
	if err != nil {
		return err
	}
	if stackName != "" {
		resolver.pseudoParameters["AWS::StackName"] = stringValue(stackName)
	}
	if inputs.NotificationARNs != nil {
		values := make([]ResolvedValue, len(inputs.NotificationARNs))
		for index, arn := range inputs.NotificationARNs {
			if arn == "" || arn != strings.TrimSpace(arn) {
				return fmt.Errorf("notification ARN %d must be non-empty and have no surrounding whitespace", index)
			}
			values[index] = stringValue(arn)
		}
		resolver.pseudoParameters["AWS::NotificationARNs"] = ResolvedValue{State: ResolutionExact, Kind: ResolvedList, List: values}
	}
	resolver.pseudoParameters["AWS::NoValue"] = ResolvedValue{State: ResolutionExact, Kind: ResolvedNoValue}

	for _, name := range sortedRawKeys(inputs.ParameterValues) {
		spec, exists := resolver.parameters[name]
		if !exists {
			return fmt.Errorf("resolved parameter value %q is not declared by the template", name)
		}
		decoded, err := decodeResolutionJSON(inputs.ParameterValues[name], resolver.limits)
		if err != nil {
			return fmt.Errorf("resolved parameter value %q: %w", name, err)
		}
		value, err := normalizeParameterValue(spec.typeName, decoded)
		if err != nil {
			return fmt.Errorf("resolved parameter value %q: %w", name, err)
		}
		resolver.parameterValues[name] = value
	}
	for _, name := range sortedBoolKeys(inputs.ConditionValues) {
		if _, exists := resolver.conditions[name]; !exists {
			return fmt.Errorf("condition value %q is not declared by the template", name)
		}
		resolver.conditionValues[name] = inputs.ConditionValues[name]
	}
	for _, logicalID := range sortedRawKeys(inputs.ResourceRefs) {
		if _, exists := resolver.resources[logicalID]; !exists {
			return fmt.Errorf("resource Ref value %q is not declared by the template", logicalID)
		}
		value, err := decodeContextLiteral(inputs.ResourceRefs[logicalID], resolver.limits)
		if err != nil {
			return fmt.Errorf("resource Ref value %q: %w", logicalID, err)
		}
		resolver.resourceRefs[logicalID] = value
	}
	resourceIDs := make([]string, 0, len(inputs.ResourceAttributes))
	for logicalID := range inputs.ResourceAttributes {
		resourceIDs = append(resourceIDs, logicalID)
	}
	sort.Strings(resourceIDs)
	for _, logicalID := range resourceIDs {
		if _, exists := resolver.resources[logicalID]; !exists {
			return fmt.Errorf("resource attribute values %q are not declared by the template", logicalID)
		}
		attributes := inputs.ResourceAttributes[logicalID]
		resolved := make(map[string]ResolvedValue, len(attributes))
		for _, attribute := range sortedRawKeys(attributes) {
			if attribute == "" || attribute != strings.TrimSpace(attribute) {
				return fmt.Errorf("resource %q attribute name must be non-empty and have no surrounding whitespace", logicalID)
			}
			value, err := decodeContextLiteral(attributes[attribute], resolver.limits)
			if err != nil {
				return fmt.Errorf("resource %q attribute %q: %w", logicalID, attribute, err)
			}
			resolved[attribute] = value
		}
		resolver.resourceAttributes[logicalID] = resolved
	}
	return nil
}

func normalizeResolutionLimits(limits ResolutionLimits) (ResolutionLimits, error) {
	defaults := DefaultResolutionLimits()
	if limits.MaxDepth == 0 {
		limits.MaxDepth = defaults.MaxDepth
	}
	if limits.MaxSteps == 0 {
		limits.MaxSteps = defaults.MaxSteps
	}
	if limits.MaxIssues == 0 {
		limits.MaxIssues = defaults.MaxIssues
	}
	if limits.MaxStringBytes == 0 {
		limits.MaxStringBytes = defaults.MaxStringBytes
	}
	if limits.MaxContextValueBytes == 0 {
		limits.MaxContextValueBytes = defaults.MaxContextValueBytes
	}
	if limits.MaxDepth < 0 || limits.MaxSteps < 0 || limits.MaxIssues < 0 || limits.MaxStringBytes < 0 || limits.MaxContextValueBytes < 0 {
		return ResolutionLimits{}, errors.New("CloudFormation resolution limits must be positive")
	}
	return limits, nil
}

func decodeResolutionJSON(raw json.RawMessage, limits ResolutionLimits) (any, error) {
	if len(raw) == 0 {
		return nil, errors.New("JSON value is empty")
	}
	if int64(len(raw)) > limits.MaxContextValueBytes {
		return nil, fmt.Errorf("JSON value exceeds the %d-byte context value limit", limits.MaxContextValueBytes)
	}
	jsonLimits := DefaultLimits()
	jsonLimits.MaxJSONDepth = limits.MaxDepth
	jsonLimits.MaxJSONValues = limits.MaxSteps
	if err := validateStrictJSON(raw, jsonLimits); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func decodeContextLiteral(raw json.RawMessage, limits ResolutionLimits) (ResolvedValue, error) {
	value, err := decodeResolutionJSON(raw, limits)
	if err != nil {
		return ResolvedValue{}, err
	}
	return literalValue(value, true)
}

func literalValue(value any, rejectIntrinsics bool) (ResolvedValue, error) {
	switch typed := value.(type) {
	case nil:
		return ResolvedValue{State: ResolutionExact, Kind: ResolvedNull}, nil
	case string:
		return stringValue(typed), nil
	case json.Number:
		return ResolvedValue{State: ResolutionExact, Kind: ResolvedNumber, Number: typed.String()}, nil
	case bool:
		return ResolvedValue{State: ResolutionExact, Kind: ResolvedBoolean, Boolean: typed}, nil
	case []any:
		values := make([]ResolvedValue, len(typed))
		for index, item := range typed {
			resolved, err := literalValue(item, rejectIntrinsics)
			if err != nil {
				return ResolvedValue{}, err
			}
			values[index] = resolved
		}
		return ResolvedValue{State: ResolutionExact, Kind: ResolvedList, List: values}, nil
	case map[string]any:
		if rejectIntrinsics {
			for key := range typed {
				if key == "Ref" || strings.HasPrefix(key, "Fn::") {
					return ResolvedValue{}, fmt.Errorf("resolved context contains intrinsic key %q", key)
				}
			}
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fields := make([]ResolvedField, 0, len(keys))
		for _, key := range keys {
			resolved, err := literalValue(typed[key], rejectIntrinsics)
			if err != nil {
				return ResolvedValue{}, err
			}
			fields = append(fields, ResolvedField{Name: key, Value: resolved})
		}
		return ResolvedValue{State: ResolutionExact, Kind: ResolvedObject, Object: fields}, nil
	default:
		return ResolvedValue{}, fmt.Errorf("unsupported JSON value type %T", value)
	}
}

func normalizeParameterValue(typeName string, raw any) (ResolvedValue, error) {
	if typeName == "CommaDelimitedList" || strings.HasPrefix(typeName, "List<") {
		switch typed := raw.(type) {
		case string:
			parts := strings.Split(typed, ",")
			values := make([]ResolvedValue, len(parts))
			for index, part := range parts {
				values[index] = stringValue(strings.TrimSpace(part))
			}
			return ResolvedValue{State: ResolutionExact, Kind: ResolvedList, List: values}, nil
		case []any:
			values := make([]ResolvedValue, len(typed))
			for index, item := range typed {
				text, ok := parameterScalar(item)
				if !ok {
					return ResolvedValue{}, fmt.Errorf("list item %d must be a string or number", index)
				}
				values[index] = stringValue(strings.TrimSpace(text))
			}
			return ResolvedValue{State: ResolutionExact, Kind: ResolvedList, List: values}, nil
		default:
			return ResolvedValue{}, fmt.Errorf("type %s requires a comma-delimited string or scalar list", typeName)
		}
	}
	text, ok := parameterScalar(raw)
	if !ok {
		return ResolvedValue{}, fmt.Errorf("type %s requires a string or number", typeName)
	}
	return stringValue(text), nil
}

func parameterScalar(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case json.Number:
		return typed.String(), true
	default:
		return "", false
	}
}

func parseNoEcho(value any) (bool, error) {
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case string:
		switch strings.ToLower(typed) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, errors.New("must be true or false")
}

func reconcileAddress(name, assembly, unknown, explicit string, pattern *regexp.Regexp) (string, error) {
	if assembly == unknown {
		assembly = ""
	}
	for _, candidate := range []string{assembly, explicit} {
		if candidate != "" && (candidate != strings.TrimSpace(candidate) || !pattern.MatchString(candidate)) {
			return "", fmt.Errorf("%s %q is invalid", name, candidate)
		}
	}
	if assembly != "" && explicit != "" && assembly != explicit {
		return "", fmt.Errorf("stack environment %s %q conflicts with explicit %s %q", name, assembly, name, explicit)
	}
	if explicit != "" {
		return explicit, nil
	}
	return assembly, nil
}

func reconcileLiteral(name, assembly, explicit string) (string, error) {
	for _, candidate := range []string{assembly, explicit} {
		if candidate != strings.TrimSpace(candidate) {
			return "", fmt.Errorf("%s must not have surrounding whitespace", name)
		}
	}
	if assembly != "" && explicit != "" && assembly != explicit {
		return "", fmt.Errorf("stack %s %q conflicts with explicit %s %q", name, assembly, name, explicit)
	}
	if explicit != "" {
		return explicit, nil
	}
	return assembly, nil
}

func stringValue(value string) ResolvedValue {
	return ResolvedValue{State: ResolutionExact, Kind: ResolvedString, String: value}
}

func sortedRawKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedBoolKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func marshalExpression(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}
