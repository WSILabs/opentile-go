package opentile

import "testing"

func TestBackingDefault(t *testing.T) {
	cfg := newConfig(nil)
	if got := cfg.backing; got != BackingAuto {
		t.Errorf("default backing = %v, want BackingAuto", got)
	}
	if cfg.hasBacking {
		t.Error("hasBacking should be false when no WithBacking opt was passed")
	}
}

func TestWithBacking(t *testing.T) {
	for _, b := range []Backing{BackingMmap, BackingPread, BackingAuto} {
		cfg := newConfig([]Option{WithBacking(b)})
		if got := cfg.backing; got != b {
			t.Errorf("WithBacking(%v): got %v", b, got)
		}
		if !cfg.hasBacking {
			t.Errorf("WithBacking(%v): hasBacking should be true", b)
		}
	}
}

func TestConfigBackingAccessor(t *testing.T) {
	cfg := newConfig([]Option{WithBacking(BackingPread)})
	c := &Config{c: cfg}
	if got := c.Backing(); got != BackingPread {
		t.Errorf("Config.Backing() = %v, want BackingPread", got)
	}
}
