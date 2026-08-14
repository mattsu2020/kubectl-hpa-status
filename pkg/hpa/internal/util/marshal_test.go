package util

import (
	"testing"
)

func TestMarshalJSON(t *testing.T) {
	t.Run("valid value", func(t *testing.T) {
		got, err := MarshalJSON(map[string]any{"a": 1})
		if err != nil {
			t.Fatalf("MarshalJSON returned error: %v", err)
		}
		if got != `{"a":1}` {
			t.Fatalf("MarshalJSON = %q, want {\"a\":1}", got)
		}
	})
	t.Run("invalid value returns error", func(t *testing.T) {
		_, err := MarshalJSON(make(chan int))
		if err == nil {
			t.Fatal("expected error for non-JSON value")
		}
	})
}

func TestMustMarshalJSON(t *testing.T) {
	t.Run("valid value", func(t *testing.T) {
		if got := MustMarshalJSON(map[string]any{"a": 1}); got != `{"a":1}` {
			t.Fatalf("MustMarshalJSON = %q, want {\"a\":1}", got)
		}
	})
	t.Run("invalid value panics", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic for non-JSON value")
			}
		}()
		_ = MustMarshalJSON(make(chan int))
	})
}
