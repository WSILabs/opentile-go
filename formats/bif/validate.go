package bif

import (
	opentile "github.com/wsilabs/opentile-go"
	"github.com/wsilabs/opentile-go/internal/tiffvalidate"
)

// probeSink adapts opentile.ValidationProbe to tiffvalidate.Sink.
type probeSink struct{ p *opentile.ValidationProbe }

func (s probeSink) OffsetOutOfBounds(level int, msg string) {
	s.p.Flag(opentile.CheckOffsetsOutOfBounds, 0, level, msg)
}
func (s probeSink) OrphanIFD(msg string) {
	s.p.Flag(opentile.CheckOrphanIFD, -1, -1, msg)
}

// Validate contributes TIFF byte-range checks. Orphan detection is disabled
// for now (always-reachable predicate) to avoid false positives until a real
// reachability map is wired.
func (t *Tiler) Validate(p *opentile.ValidationProbe) {
	if t.file == nil {
		return
	}
	tiffvalidate.Check(t.file, probeSink{p}, func(int64) int { return -1 }, func(int64) bool { return true })
}
