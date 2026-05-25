package szi

// image.go previously defined the *image type implementing opentile.Image.
// In v0.24, opentile.Image became a value-type struct; the szi Tiler now
// stores t.sziImage (opentile.Image) directly. This file is intentionally
// empty — retained to avoid stale-file merge conflicts.
