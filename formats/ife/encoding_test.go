package ife

import (
	"strings"
	"testing"

	opentile "github.com/wsilabs/opentile-go"
)

func TestCompressionFromEncoding(t *testing.T) {
	for _, tc := range []struct {
		name        string
		in          uint8
		want        opentile.Compression
		wantErrSub  string
	}{
		{"iris", encodingIRIS, opentile.CompressionIRIS, ""},
		{"jpeg", encodingJPEG, opentile.CompressionJPEG, ""},
		{"avif", encodingAVIF, opentile.CompressionAVIF, ""},
		{"undefined", encodingUndefined, opentile.CompressionUnknown, "TILE_ENCODING_UNDEFINED"},
		{"unknown-7", 7, opentile.CompressionUnknown, "unknown encoding 7"},
		{"unknown-255", 255, opentile.CompressionUnknown, "unknown encoding 255"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := compressionFromEncoding(tc.in)
			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Errorf("err = %q; want substring %q", err, tc.wantErrSub)
				}
			} else if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Errorf("compression = %v, want %v", got, tc.want)
			}
		})
	}
}
