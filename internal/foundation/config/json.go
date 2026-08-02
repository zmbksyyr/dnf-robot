package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

const defaultMaxJSONDocumentBytes int64 = 16 << 20

// DecodeJSON strictly decodes exactly one JSON value. Unknown object fields
// and trailing JSON values are rejected. The input is bounded to protect
// callers that decode local or remote content through this shared helper.
func DecodeJSON(r io.Reader, dst any) error {
	return DecodeJSONLimit(r, defaultMaxJSONDocumentBytes, dst)
}

// DecodeJSONLimit is DecodeJSON with an explicit input size limit. One extra
// byte is read so a document that is exactly valid up to the limit but has
// more data is rejected deterministically.
func DecodeJSONLimit(r io.Reader, maxBytes int64, dst any) error {
	if maxBytes <= 0 {
		return fmt.Errorf("JSON size limit must be positive")
	}
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > maxBytes {
		return fmt.Errorf("JSON document exceeds %d-byte limit", maxBytes)
	}
	return DecodeJSONBytes(data, dst)
}

// DecodeJSONBytes strictly decodes one already-buffered JSON document without
// making another copy of the document.
func DecodeJSONBytes(data []byte, dst any) error {
	if int64(len(data)) > defaultMaxJSONDocumentBytes {
		return fmt.Errorf("JSON document exceeds %d-byte limit", defaultMaxJSONDocumentBytes)
	}
	if err := validateJSONKeys(data, reflect.TypeOf(dst)); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

var jsonRawMessageType = reflect.TypeOf(json.RawMessage{})

func validateJSONKeys(data []byte, target reflect.Type) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var walk func(reflect.Type) error
	walk = func(target reflect.Type) error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		target = indirectJSONType(target)
		if target == jsonRawMessageType || target != nil && target.Kind() == reflect.Interface {
			target = nil
		}
		switch delim {
		case '{':
			fields := jsonStructFields(target)
			var mapValue reflect.Type
			if target != nil && target.Kind() == reflect.Map {
				mapValue = target.Elem()
			}
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("JSON object key is not a string")
				}
				if _, duplicate := seen[key]; duplicate {
					return fmt.Errorf("duplicate JSON object key %q", key)
				}
				seen[key] = struct{}{}
				valueType := mapValue
				if fields != nil {
					var known bool
					valueType, known = fields[key]
					if !known {
						return fmt.Errorf("unknown JSON object key %q", key)
					}
				}
				if err := walk(valueType); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			var element reflect.Type
			if target != nil && (target.Kind() == reflect.Slice || target.Kind() == reflect.Array) {
				element = target.Elem()
			}
			for decoder.More() {
				if err := walk(element); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delim)
		}
	}
	return walk(target)
}

func indirectJSONType(target reflect.Type) reflect.Type {
	for target != nil && target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	return target
}

func jsonStructFields(target reflect.Type) map[string]reflect.Type {
	if target == nil || target.Kind() != reflect.Struct || target == jsonRawMessageType {
		return nil
	}
	fields := make(map[string]reflect.Type)
	var add func(reflect.Type)
	add = func(current reflect.Type) {
		current = indirectJSONType(current)
		if current == nil || current.Kind() != reflect.Struct {
			return
		}
		for index := 0; index < current.NumField(); index++ {
			field := current.Field(index)
			if field.PkgPath != "" {
				continue
			}
			tag := field.Tag.Get("json")
			name := strings.Split(tag, ",")[0]
			if name == "-" {
				continue
			}
			if field.Anonymous && name == "" {
				add(field.Type)
				continue
			}
			if name == "" {
				name = field.Name
			}
			fields[name] = field.Type
		}
	}
	add(target)
	return fields
}
