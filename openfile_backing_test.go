package opentile_test

// openfile_backing_test.go previously tested BackingMmap vs BackingPread parity
// using Level.Tile / Level.Grid / Level.Size interface methods. Those methods
// were removed in v0.24 when Level became a value-type struct. Tests will be
// rewritten in Phase 3 to use *Slide.RawTile + value-type Level fields.
