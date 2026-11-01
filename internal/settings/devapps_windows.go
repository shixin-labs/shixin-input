package settings

// DevApps are terminals and code editors: offered as English-mode rules in
// one click, and without auto spacing by default. On Windows apps are
// identified by AppUserModelID; the engine also resolves desktop picker IDs
// to their executable paths when the app supplies no explicit ID.
var DevApps = []string{
	"Microsoft.WindowsTerminal_8wekyb3d8bbwe!App", "Microsoft.VisualStudioCode",
}
