package update

import "runtime"

// AssetName selects the native Windows installer, including on ARM64.
const AssetName = "ShixinSetup-" + runtime.GOARCH + ".exe"
