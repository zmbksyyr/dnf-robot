package repository

import (
	"errors"
	"reflect"
	"testing"
)

func TestOptionalCharacterInitializersContinueAfterFailure(t *testing.T) {
	initializers := []characterInitializer{
		{table: "optional_a", query: "a"},
		{table: "optional_b", query: "b"},
		{table: "optional_c", query: "c"},
	}
	var executed []string
	var reported []string
	applyOptionalCharacterInitializers(initializers, func(query string, _ ...interface{}) error {
		executed = append(executed, query)
		if query == "b" {
			return errors.New("schema mismatch")
		}
		return nil
	}, func(table string, _ error) {
		reported = append(reported, table)
	})

	if !reflect.DeepEqual(executed, []string{"a", "b", "c"}) {
		t.Fatalf("executed = %v, want all optional initializers", executed)
	}
	if !reflect.DeepEqual(reported, []string{"optional_b"}) {
		t.Fatalf("reported = %v, want only optional_b", reported)
	}
}
