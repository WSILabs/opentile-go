package opentile

import (
	"github.com/wsilabs/opentile-go/decoder"
)

// decodeWorker pulls tileKey requests from tileReqs, decodes via
// imageDecodedTile(WithScale), and stores into the cache.
//
// Exits when cancelCtx fires (Close cancels it). tileReqs is never
// closed, so workers idle on the channel between work and shutdown
// rather than exiting on a close signal — this keeps the channel
// sender-safe (no send-on-closed-channel race with Next/lookahead).
func (it *StripIterator) decodeWorker() {
	defer it.workersDone.Done()
	for {
		select {
		case k := <-it.tileReqs:
			it.decodeAndStore(k)
		case <-it.cancelCtx.Done():
			return
		}
	}
}

// decodeAndStore decodes one tile and stores it in the cache (with
// any error). The caller (lookahead) has already reserve()'d the
// key, so this is unconditional put.
func (it *StripIterator) decodeAndStore(k tileKey) {
	opts := []DecodeOption{WithFormat(decoder.PixelFormatRGB)}
	if it.idctScale > 1 {
		opts = append(opts, WithScale(it.idctScale))
	}
	img, err := it.slide.imageDecodedTile(it.imageIdx, it.sourceLevel.Index, k.tx, k.ty, opts...)
	it.cache.put(k, img, err)
}

// lookahead walks output strips in order, enqueueing tile decode
// requests for strips within [currentStrip, currentStrip + lookahead].
// Exits when cancelCtx fires or all strips' tiles have been enqueued.
func (it *StripIterator) lookahead() {
	defer it.lookaheadDone.Done()

	dispatchedStrip := 0 // last strip whose tiles we've enqueued

	for {
		// Compute the cap of strips we should have enqueued for, given
		// the current consumer position.
		it.mu.Lock()
		consumerStrip := it.nextStrip
		closed := it.closed
		it.mu.Unlock()
		if closed {
			return
		}
		targetCap := consumerStrip + it.cfg.lookahead

		// Enqueue tiles for strips up through targetCap (inclusive).
		for dispatchedStrip <= targetCap && dispatchedStrip < it.stripsTotal {
			tiles := it.tilesForStrip(dispatchedStrip)
			for _, k := range tiles {
				if !it.cache.reserve(k) {
					continue // already requested or cached
				}
				select {
				case it.tileReqs <- k:
				case <-it.cancelCtx.Done():
					return
				}
			}
			dispatchedStrip++
		}

		if dispatchedStrip >= it.stripsTotal {
			return // all done
		}

		// Wait for consumer to advance.
		select {
		case <-it.advance:
		case <-it.cancelCtx.Done():
			return
		}
	}
}

// tilesForStrip computes the set of source-level tile keys that
// overlap the given output strip's source-level coverage.
func (it *StripIterator) tilesForStrip(stripIdx int) []tileKey {
	// Output strip rows: [stripIdx*stripHeight, min((stripIdx+1)*stripHeight, outSize.Y))
	outY0 := stripIdx * it.stripHeight
	outY1 := outY0 + it.stripHeight
	if outY1 > it.outSize.Y {
		outY1 = it.outSize.Y
	}

	// Effective (codec-scaled) source-level row range covering this strip.
	scaleY := float64(it.l0Rect.Dy()) / (it.effDownsample * float64(it.outSize.Y))
	levelY0 := int(float64(outY0)*scaleY) + int(float64(it.l0Rect.Min.Y)/it.effDownsample)
	levelY1 := int(float64(outY1)*scaleY) + int(float64(it.l0Rect.Min.Y)/it.effDownsample) + 1

	// Effective column range covers the full L0 x-range.
	scaleX := 1.0 / it.effDownsample
	levelX0 := int(float64(it.l0Rect.Min.X) * scaleX)
	levelX1 := int(float64(it.l0Rect.Max.X)*scaleX) + 1

	// Clip to effective level bounds.
	if levelY0 < 0 {
		levelY0 = 0
	}
	if levelY1 > it.effLevelH {
		levelY1 = it.effLevelH
	}
	if levelX0 < 0 {
		levelX0 = 0
	}
	if levelX1 > it.effLevelW {
		levelX1 = it.effLevelW
	}

	if levelY0 >= levelY1 || levelX0 >= levelX1 {
		return nil
	}

	// Layout-aware tile selection (#60): for readers with a non-regular tile
	// grid (e.g. BIF with overlapping tiles), use TilesIntersecting to get the
	// correct set. The effective-domain coords must be scaled back up to level
	// coords (multiply by idctScale) for the regionLayout query.
	if rl := it.layout; rl != nil {
		es := it.idctScale
		if es < 1 {
			es = 1
		}
		lvlX0, lvlY0 := levelX0*es, levelY0*es
		lvlW, lvlH := (levelX1-levelX0)*es, (levelY1-levelY0)*es
		if lvlW <= 0 || lvlH <= 0 {
			return nil
		}
		tps := rl.TilesIntersecting(it.sourceLevel.Index, lvlX0, lvlY0, lvlW, lvlH)
		sub, isSub := rl.(subtileLayout)
		keys := make([]tileKey, 0, len(tps))
		seen := make(map[tileKey]struct{}, len(tps))
		for _, tp := range tps {
			col, row := tp.Col, tp.Row
			if isSub {
				// Subtile units (BIF reduced levels) share a stored source tile;
				// decode the SOURCE, deduplicated, not the per-frame unit index.
				col, row, _, _ = sub.SubtileSource(it.sourceLevel.Index, tp.Col, tp.Row)
			}
			k := tileKey{tx: col, ty: row}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
		return keys
	}

	tileW := it.effTileW
	tileH := it.effTileH
	if tileW <= 0 || tileH <= 0 {
		return nil
	}

	txMin := levelX0 / tileW
	tyMin := levelY0 / tileH
	txMax := (levelX1 - 1) / tileW
	tyMax := (levelY1 - 1) / tileH

	keys := make([]tileKey, 0, (txMax-txMin+1)*(tyMax-tyMin+1))
	for ty := tyMin; ty <= tyMax; ty++ {
		for tx := txMin; tx <= txMax; tx++ {
			keys = append(keys, tileKey{tx: tx, ty: ty})
		}
	}
	return keys
}
