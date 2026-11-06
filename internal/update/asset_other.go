//go:build !windows

package update

// AssetName is the release artifact for this platform.
const AssetName = "Shixin.dmg"

func versionedAssetName(version string) string {
	return "Shixin-" + version + ".dmg"
}
