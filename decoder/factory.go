package decoder

import "sync"

// Factory constructs decoders for a specific codec. Codec subpackages
// register a Factory in their init() function.
type Factory interface {
	// Name is the canonical codec identifier (e.g., "jpeg",
	// "jpeg2000", "lzw"). Lowercase.
	Name() string

	// TIFFCompressionTags lists the TIFF Compression tag values this
	// factory's decoder handles. Multiple tags allowed (e.g., JPEG
	// 2000 is both 33003 (Aperio) and 34712 (libtiff)). Empty for
	// non-TIFF-associated codecs.
	TIFFCompressionTags() []uint16

	// New returns a fresh Decoder instance. Each call returns a new
	// instance with its own state. Decoders are NOT safe for
	// concurrent use across goroutines.
	New() Decoder
}

var (
	regMu     sync.RWMutex
	byName    = map[string]Factory{}
	byTIFFTag = map[uint16]Factory{}
)

// Register adds a factory to the global decoder registry. Called from
// each codec subpackage's init(). Last-in-wins on name or tag
// collision (intentional — lets consumers shadow a default decoder
// with a custom impl).
func Register(f Factory) {
	regMu.Lock()
	defer regMu.Unlock()
	byName[f.Name()] = f
	for _, tag := range f.TIFFCompressionTags() {
		byTIFFTag[tag] = f
	}
}

// Get returns the factory registered for the given codec name, or
// (nil, false) if none is registered.
func Get(name string) (Factory, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	f, ok := byName[name]
	return f, ok
}

// GetByCompressionTag returns the factory registered for the given
// TIFF Compression tag value, or (nil, false) if none is registered.
func GetByCompressionTag(tag uint16) (Factory, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	f, ok := byTIFFTag[tag]
	return f, ok
}

// AvailabilityReporter is implemented by a Factory that may not be able to
// decode in this build. The cgo codec subpackages register a stub factory
// when the codec is built out (cgo disabled, -tags nocgo or no<codec>); its
// Available reports false and its decoders return ErrCodecUnavailable. A
// factory without the method is taken to be available.
type AvailabilityReporter interface {
	Available() bool
}

// AvailableByCompressionTag reports whether a decoder that works in this
// build is registered for the TIFF Compression tag value: one is registered
// and it is not a built-out stub.
func AvailableByCompressionTag(tag uint16) bool {
	f, ok := GetByCompressionTag(tag)
	if !ok {
		return false
	}
	if a, ok := f.(AvailabilityReporter); ok {
		return a.Available()
	}
	return true
}

// Registered returns the canonical names of every registered decoder.
// Order is unspecified.
func Registered() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(byName))
	for n := range byName {
		out = append(out, n)
	}
	return out
}
