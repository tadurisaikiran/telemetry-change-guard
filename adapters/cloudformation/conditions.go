package cloudformation

import (
	"fmt"
	"strings"
)

type conditionEvaluation struct {
	known bool
	value bool
}

func (state *evaluationState) evaluateCondition(name, valuePath string, depth int) (conditionEvaluation, error) {
	if err := state.ctx.Err(); err != nil {
		return conditionEvaluation{}, err
	}
	if value, supplied := state.resolver.conditionValues[name]; supplied {
		return conditionEvaluation{known: true, value: value}, nil
	}
	expression, exists := state.resolver.conditions[name]
	if !exists {
		state.addIssue("UNKNOWN_CONDITION", valuePath, fmt.Sprintf("condition %q is not declared and has no explicit value", name))
		return conditionEvaluation{}, nil
	}
	if _, cycling := state.conditionStack[name]; cycling {
		state.addIssue("CONDITION_CYCLE", valuePath, fmt.Sprintf("condition reference cycle includes %q", name))
		return conditionEvaluation{}, nil
	}
	if depth > state.resolver.limits.MaxDepth {
		state.addIssue("EVALUATION_LIMIT", valuePath, fmt.Sprintf("condition evaluation exceeds the depth limit of %d", state.resolver.limits.MaxDepth))
		return conditionEvaluation{}, nil
	}
	state.conditionStack[name] = struct{}{}
	defer delete(state.conditionStack, name)
	return state.evaluateConditionExpression(expression, valuePath+"/"+escapeJSONPointer(name), depth+1)
}

func (state *evaluationState) evaluateConditionExpression(expression any, valuePath string, depth int) (conditionEvaluation, error) {
	if err := state.ctx.Err(); err != nil {
		return conditionEvaluation{}, err
	}
	state.steps++
	if state.steps > state.resolver.limits.MaxSteps {
		state.addIssue("EVALUATION_LIMIT", valuePath, fmt.Sprintf("condition evaluation exceeds the step limit of %d", state.resolver.limits.MaxSteps))
		return conditionEvaluation{}, nil
	}
	if literal, ok := expression.(bool); ok {
		return conditionEvaluation{known: true, value: literal}, nil
	}
	object, ok := expression.(map[string]any)
	if !ok || len(object) != 1 {
		state.addIssue("INVALID_CONDITION", valuePath, "condition expression must contain exactly one supported condition function")
		return conditionEvaluation{}, nil
	}
	for function, argument := range object {
		functionPath := valuePath + "/" + escapeJSONPointer(function)
		switch function {
		case "Condition":
			name, ok := argument.(string)
			if !ok || name == "" || name != strings.TrimSpace(name) {
				state.addIssue("INVALID_CONDITION", functionPath, "Condition requires a non-empty literal condition name")
				return conditionEvaluation{}, nil
			}
			return state.evaluateCondition(name, functionPath, depth+1)
		case "Fn::Equals":
			return state.evaluateEqualsCondition(argument, functionPath, depth+1)
		case "Fn::And":
			return state.evaluateLogicalCondition(argument, functionPath, depth+1, true)
		case "Fn::Or":
			return state.evaluateLogicalCondition(argument, functionPath, depth+1, false)
		case "Fn::Not":
			arguments, ok := argument.([]any)
			if !ok || len(arguments) != 1 {
				state.addIssue("INVALID_CONDITION", functionPath, "Fn::Not requires exactly one condition")
				return conditionEvaluation{}, nil
			}
			child, err := state.evaluateConditionExpression(arguments[0], functionPath+"/0", depth+1)
			if err != nil || !child.known {
				return conditionEvaluation{}, err
			}
			return conditionEvaluation{known: true, value: !child.value}, nil
		case "Fn::If":
			resolved, err := state.evaluateIf(argument, functionPath, depth+1)
			if err != nil {
				return conditionEvaluation{}, err
			}
			if resolved.value.State == ResolutionExact && resolved.value.Kind == ResolvedBoolean {
				return conditionEvaluation{known: true, value: resolved.value.Boolean}, nil
			}
			state.addIssue("UNRESOLVED_CONDITION", functionPath, "Fn::If condition result is not an exact boolean")
			return conditionEvaluation{}, nil
		default:
			state.addIssue("UNSUPPORTED_CONDITION", functionPath, fmt.Sprintf("condition function %s is not supported", function))
			return conditionEvaluation{}, nil
		}
	}
	return conditionEvaluation{}, nil
}

func (state *evaluationState) evaluateEqualsCondition(argument any, valuePath string, depth int) (conditionEvaluation, error) {
	arguments, ok := argument.([]any)
	if !ok || len(arguments) != 2 {
		state.addIssue("INVALID_CONDITION", valuePath, "Fn::Equals requires exactly two values")
		return conditionEvaluation{}, nil
	}
	values := make([]string, 2)
	for index, argument := range arguments {
		resolved, err := state.evaluate(argument, fmt.Sprintf("%s/%d", valuePath, index), depth+1)
		if err != nil {
			return conditionEvaluation{}, err
		}
		value, scalar := exactScalarString(resolved.value)
		if resolved.value.State != ResolutionExact || !scalar {
			state.addIssue("UNRESOLVED_CONDITION", fmt.Sprintf("%s/%d", valuePath, index), "Fn::Equals operands must resolve to exact strings or numbers")
			return conditionEvaluation{}, nil
		}
		values[index] = value
	}
	return conditionEvaluation{known: true, value: values[0] == values[1]}, nil
}

func (state *evaluationState) evaluateLogicalCondition(argument any, valuePath string, depth int, and bool) (conditionEvaluation, error) {
	arguments, ok := argument.([]any)
	if !ok || len(arguments) < 2 || len(arguments) > 10 {
		state.addIssue("INVALID_CONDITION", valuePath, "Fn::And and Fn::Or require between 2 and 10 conditions")
		return conditionEvaluation{}, nil
	}
	unknown := false
	for index, argument := range arguments {
		child, err := state.evaluateConditionExpression(argument, fmt.Sprintf("%s/%d", valuePath, index), depth+1)
		if err != nil {
			return conditionEvaluation{}, err
		}
		if !child.known {
			unknown = true
			continue
		}
		if and && !child.value {
			return conditionEvaluation{known: true, value: false}, nil
		}
		if !and && child.value {
			return conditionEvaluation{known: true, value: true}, nil
		}
	}
	if unknown {
		return conditionEvaluation{}, nil
	}
	return conditionEvaluation{known: true, value: and}, nil
}
