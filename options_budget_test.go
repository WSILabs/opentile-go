package opentile

import (
	"runtime/debug"
	"testing"
)

func TestMemoryBudgetDefault(t *testing.T) {
	c := newConfig(nil)
	if got := c.resolveMemoryBudget(); got != defaultReadMemoryBudget {
		t.Fatalf("default budget = %d, want %d", got, defaultReadMemoryBudget)
	}
}

func TestMemoryBudgetOptionOverridesEnv(t *testing.T) {
	t.Setenv("OPENTILE_READ_MEMORY_BUDGET", "500000000")
	c := newConfig([]Option{WithMemoryBudget(700_000_000)})
	if got := c.resolveMemoryBudget(); got != 700_000_000 {
		t.Fatalf("option should win over env: got %d, want 700000000", got)
	}
}

func TestMemoryBudgetEnvUsedWhenNoOption(t *testing.T) {
	t.Setenv("OPENTILE_READ_MEMORY_BUDGET", "500000000")
	c := newConfig(nil)
	if got := c.resolveMemoryBudget(); got != 500_000_000 {
		t.Fatalf("env budget = %d, want 500000000", got)
	}
}

func TestMemoryBudgetIgnoresGarbageEnv(t *testing.T) {
	t.Setenv("OPENTILE_READ_MEMORY_BUDGET", "not-a-number")
	c := newConfig(nil)
	if got := c.resolveMemoryBudget(); got != defaultReadMemoryBudget {
		t.Fatalf("garbage env should fall back to default: got %d", got)
	}
}

func TestMemoryBudgetShrinksUnderGOMEMLIMIT(t *testing.T) {
	// Set a 1 GiB runtime limit; restore after.
	prev := debug.SetMemoryLimit(1 << 30)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })

	c := newConfig(nil) // no explicit budget
	got := c.resolveMemoryBudget()
	// Should be <= half the limit (leave headroom for GC + C2/C3 + app),
	// and never below the 128 MiB floor.
	if got > (1<<30)/2 {
		t.Fatalf("budget %d should be <= half of GOMEMLIMIT", got)
	}
	if got < 128<<20 {
		t.Fatalf("budget %d below floor", got)
	}
}

func TestExplicitBudgetIgnoresGOMEMLIMIT(t *testing.T) {
	prev := debug.SetMemoryLimit(1 << 30)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	c := newConfig([]Option{WithMemoryBudget(900_000_000)})
	if got := c.resolveMemoryBudget(); got != 900_000_000 {
		t.Fatalf("explicit budget must be honoured verbatim: got %d", got)
	}
}
