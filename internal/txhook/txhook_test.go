package txhook

import (
	"context"
	"testing"
)

func TestAfterCommitRunsImmediatelyWithoutTransaction(t *testing.T) {
	ran := false
	AfterCommit(context.Background(), func() { ran = true })
	if !ran {
		t.Fatal("hook must run immediately outside a transaction")
	}
}

func TestAfterCommitDefersUntilCollected(t *testing.T) {
	ctx, collect := With(context.Background())
	order := []int{}
	AfterCommit(ctx, func() { order = append(order, 1) })
	AfterCommit(ctx, func() { order = append(order, 2) })
	if len(order) != 0 {
		t.Fatalf("hooks ran early: %v", order)
	}
	for _, hook := range collect() {
		hook()
	}
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("order=%v", order)
	}
	if len(collect()) != 0 {
		t.Fatal("hooks must be collected once")
	}
}

func TestNestedScopeSharesOuterCollector(t *testing.T) {
	outer, collectOuter := With(context.Background())
	inner, collectInner := With(outer)
	AfterCommit(inner, func() {})
	if len(collectInner()) != 0 {
		t.Fatal("inner scope must not own hooks")
	}
	if len(collectOuter()) != 1 {
		t.Fatal("outer scope must own hooks")
	}
}
