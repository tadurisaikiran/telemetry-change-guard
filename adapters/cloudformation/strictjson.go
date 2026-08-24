package cloudformation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

type jsonBudget struct {
	maxDepth  int
	maxValues int
	values    int
}

func validateStrictJSON(contents []byte, limits Limits) error {
	if !utf8.Valid(contents) {
		return errors.New("input is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	budget := jsonBudget{maxDepth: limits.MaxJSONDepth, maxValues: limits.MaxJSONValues}
	if err := budget.value(decoder, 1); err != nil {
		return err
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			return fmt.Errorf("decode trailing JSON: %w", err)
		}
		return fmt.Errorf("input contains more than one JSON value (next token %v)", token)
	}
	return nil
}

func isJSONNull(raw []byte) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func (budget *jsonBudget) value(decoder *json.Decoder, depth int) error {
	if depth > budget.maxDepth {
		return fmt.Errorf("JSON nesting exceeds the depth limit of %d", budget.maxDepth)
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode JSON value: %w", err)
	}
	if err := budget.consume(); err != nil {
		return err
	}

	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("decode JSON object key: %w", err)
			}
			if err := budget.consume(); err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key has unexpected type %T", keyToken)
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("JSON object contains duplicate key %q", key)
			}
			seen[key] = struct{}{}
			if err := budget.value(decoder, depth+1); err != nil {
				return fmt.Errorf("JSON key %q: %w", key, err)
			}
		}
		return budget.close(decoder, '}')
	case '[':
		for decoder.More() {
			if err := budget.value(decoder, depth+1); err != nil {
				return err
			}
		}
		return budget.close(decoder, ']')
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

func (budget *jsonBudget) consume() error {
	budget.values++
	if budget.values > budget.maxValues {
		return fmt.Errorf("JSON token count exceeds the limit of %d", budget.maxValues)
	}
	return nil
}

func (budget *jsonBudget) close(decoder *json.Decoder, expected json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode closing JSON delimiter: %w", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != expected {
		return fmt.Errorf("expected closing JSON delimiter %q, got %v", expected, token)
	}
	return budget.consume()
}
