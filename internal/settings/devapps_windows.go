package settings

// DevApps are terminals and code editors: offered as English-mode rules in
// one click, and without auto spacing by default. On Windows apps are
// identified by AppUserModelID; desktop apps without an explicit one aren't
// mapped yet (KNOWN-ISSUES, Windows).
var DevApps = []string{
	"Microsoft.WindowsTerminal_8wekyb3d8bbwe!App", "Microsoft.VisualStudioCode",
}
