package opentile

import "testing"

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
