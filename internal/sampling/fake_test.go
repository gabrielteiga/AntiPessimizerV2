package sampling

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"
)

func TestDeterminismEmptyAndCancellation(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, n := range []int{0, 10} {
		a, _ := NewFake(start, time.Second, 2, n)
		b, _ := NewFake(start, time.Second, 2, n)
		count := 0
		for {
			e, err := a.Next(context.Background())
			other, otherErr := b.Next(context.Background())
			if !reflect.DeepEqual(e, other) || !errors.Is(err, otherErr) {
				t.Fatal("nondeterministic source")
			}
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			count++
		}
		if count != 2*(n+1) {
			t.Fatal("wrong events", count)
		}
	}
	f, _ := NewFake(start, time.Second, 1, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := NewFake(start, time.Second, 1, -1); err == nil {
		t.Fatal("accepted negative samples")
	}
}
