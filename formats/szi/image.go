package szi

// image.go previously defined the *image type implementing opentile.Image.
// In v0.24, opentile.Image became a value-type struct (now renamed Pyramid);
// the szi Tiler stores t.sziImage (opentile.Pyramid) directly. This file is
// intentionally empty — retained to avoid stale-file merge conflicts.
