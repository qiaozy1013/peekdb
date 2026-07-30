package inspector_test

import (
	"testing"

	"github.com/qiaozy1013/peekdb/internal/inspector"
)

// These tests are mostly type-level guarantees: peekdb's TUI/CLI
// will read Item/Row/Column fields by name, and a rename would
// break the v1 contract. The tests pin the public surface so
// refactors cannot silently move fields.

func TestItem_PublicFields(t *testing.T) {
	it := inspector.Item{
		Name:     "users",
		Kind:     "table",
		Size:     4096,
		Count:    100,
		Children: []string{"users/active", "users/disabled"},
		Meta:     map[string]string{"sql": "CREATE TABLE users(id INT)"},
	}
	if it.Name != "users" {
		t.Errorf("Name = %q", it.Name)
	}
	if it.Kind != "table" {
		t.Errorf("Kind = %q", it.Kind)
	}
	if it.Size != 4096 {
		t.Errorf("Size = %d", it.Size)
	}
	if it.Count != 100 {
		t.Errorf("Count = %d", it.Count)
	}
	if len(it.Children) != 2 {
		t.Errorf("Children len = %d", len(it.Children))
	}
	if it.Meta["sql"] == "" {
		t.Errorf("Meta[sql] empty")
	}
}

func TestItem_NilMetaIsSafe(t *testing.T) {
	// Reading from a nil Meta must not panic — TUI code does
	// `if item.Meta["x"] != ""` and expects a zero value.
	var it inspector.Item
	if got := it.Meta["anything"]; got != "" {
		t.Errorf("nil Meta read = %q, want empty", got)
	}
}

func TestColumn_PublicFields(t *testing.T) {
	c := inspector.Column{
		Name:     "id",
		Type:     "INTEGER",
		Nullable: false,
	}
	if c.Name != "id" || c.Type != "INTEGER" || c.Nullable {
		t.Errorf("Column fields not set: %+v", c)
	}
}

func TestRow_KVLayout(t *testing.T) {
	r := inspector.Row{
		Key:   []byte("user:42"),
		Value: []byte(`{"id":42}`),
	}
	if string(r.Key) != "user:42" {
		t.Errorf("Key = %q", r.Key)
	}
	if string(r.Value) != `{"id":42}` {
		t.Errorf("Value = %q", r.Value)
	}
	if r.Columns != nil || r.Values != nil {
		t.Errorf("KV row should have nil Columns/Values, got %+v", r)
	}
}

func TestRow_SQLLayout(t *testing.T) {
	r := inspector.Row{
		Columns: []inspector.Column{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "TEXT"},
		},
		Values: []any{int64(42), "alice"},
	}
	if len(r.Columns) != 2 {
		t.Errorf("Columns len = %d", len(r.Columns))
	}
	if len(r.Values) != 2 {
		t.Errorf("Values len = %d", len(r.Values))
	}
	if r.Key != nil || r.Value != nil {
		t.Errorf("SQL row should have nil Key/Value, got %+v", r)
	}
	// Values type-erasure is the price of using any. Smoke test
	// the common SQLite types.
	switch v := r.Values[0].(type) {
	case int64:
		if v != 42 {
			t.Errorf("Values[0] = %d, want 42", v)
		}
	default:
		t.Errorf("Values[0] type = %T, want int64", v)
	}
}
