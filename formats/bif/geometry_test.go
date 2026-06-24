package bif

import (
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

func TestFloorHalveSize(t *testing.T) {
	// Ventana-1 DP hull → bio-formats sizeX/sizeY chain.
	hull := opentile.Size{W: 23432, H: 21504}
	wantW := []int{23432, 11716, 5858, 2929, 1464, 732, 366, 183}
	wantH := []int{21504, 10752, 5376, 2688, 1344, 672, 336, 168}
	for i := range wantW {
		got := floorHalveSize(hull, i)
		if got.W != wantW[i] || got.H != wantH[i] {
			t.Errorf("floorHalveSize(hull,%d) = %dx%d, want %dx%d", i, got.W, got.H, wantW[i], wantH[i])
		}
	}
}
