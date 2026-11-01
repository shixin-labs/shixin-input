package main

// platform is what differs between macOS and Windows in the settings app.
// Pages, widgets, layout and strings are shared; each OS's file sets this.
type platform struct {
	// front finds the input method's front executable beside this app; ""
	// if it isn't there (a development build).
	front func() string
	// added: the front answers -added and -repair, so the guide shows
	// whether 拾新 is in the user's input sources and repairs stale caches.
	added bool
	// consent: the OS asks the user to allow a new input method (macOS),
	// the guide's first step.
	consent bool
	// selfUpdate: the front verifies and applies a downloaded release
	// (-apply-update). Elsewhere 关于 links to the download page.
	selfUpdate bool
	// inputSettings opens the OS's input-source settings; refresh runs the
	// front's -refresh first (macOS's stale list, DESIGN §5.5).
	inputSettings string
	refresh       bool
	// guide holds the 开始使用 page's i18n keys for this OS.
	guide guideKeys
}

type guideKeys struct {
	subtitle, addStep, add, addDesc, addOpen, typeStep, typeDesc string
}

// The 开始使用 guide's keys: macOS asks for consent first (step 1), Windows
// doesn't, so its steps are numbered from adding.
var (
	macGuide = guideKeys{
		subtitle: "s.start.subtitle", addStep: "s.start.step2", add: "s.start.add",
		addDesc: "s.start.add.desc", addOpen: "s.start.add.open",
		typeStep: "s.start.step3", typeDesc: "s.start.type.desc",
	}
	windowsGuide = guideKeys{
		subtitle: "s.start.win.subtitle", addStep: "s.start.win.step1", add: "s.start.win.add",
		addDesc: "s.start.win.add.desc", addOpen: "s.start.win.add.open",
		typeStep: "s.start.win.step2", typeDesc: "s.start.win.type.desc",
	}
)

// releasesPage is where releases are downloaded by hand.
const releasesPage = "https://github.com/shixin-labs/shixin-input/releases/latest"
