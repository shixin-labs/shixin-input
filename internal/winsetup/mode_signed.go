//go:build !shixin_unsigned_preview

package winsetup

// UnsignedPreview is a build-time identity, never a runtime verification flag.
const UnsignedPreview = false
