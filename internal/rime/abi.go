package rime

// C layouts mirroring rime_api.h (librime >= 1.10) on 64-bit targets.
//
// Field order and widths MUST match the header: C `int`/`Bool` is int32 and
// every pointer is uintptr. Go applies the same natural alignment as C, so
// the implicit padding matches.

type cTraits struct {
	dataSize             int32
	sharedDataDir        uintptr
	userDataDir          uintptr
	distributionName     uintptr
	distributionCodeName uintptr
	distributionVersion  uintptr
	appName              uintptr
	modules              uintptr
	minLogLevel          int32
	logDir               uintptr
	prebuiltDataDir      uintptr
	stagingDir           uintptr
}

type cComposition struct {
	length    int32
	cursorPos int32
	selStart  int32
	selEnd    int32
	preedit   uintptr
}

type cCandidate struct {
	text     uintptr
	comment  uintptr
	reserved uintptr
}

type cMenu struct {
	pageSize                  int32
	pageNo                    int32
	isLastPage                int32
	highlightedCandidateIndex int32
	numCandidates             int32
	candidates                uintptr
	selectKeys                uintptr
}

type cCommit struct {
	dataSize int32
	text     uintptr
}

type cContext struct {
	dataSize          int32
	composition       cComposition
	menu              cMenu
	commitTextPreview uintptr
	selectLabels      uintptr
}

type cStatus struct {
	dataSize      int32
	schemaID      uintptr
	schemaName    uintptr
	isDisabled    int32
	isComposing   int32
	isASCIIMode   int32
	isFullShape   int32
	isSimplified  int32
	isTraditional int32
	isASCIIPunct  int32
}

// cAPI mirrors RimeApi: data_size followed by function pointers, in header
// order. Only members covered by data_size may be used.
type cAPI struct {
	dataSize                        int32
	setup                           uintptr
	setNotificationHandler          uintptr
	initialize                      uintptr
	finalize                        uintptr
	startMaintenance                uintptr
	isMaintenanceMode               uintptr
	joinMaintenanceThread           uintptr
	deployerInitialize              uintptr
	prebuild                        uintptr
	deploy                          uintptr
	deploySchema                    uintptr
	deployConfigFile                uintptr
	syncUserData                    uintptr
	createSession                   uintptr
	findSession                     uintptr
	destroySession                  uintptr
	cleanupStaleSessions            uintptr
	cleanupAllSessions              uintptr
	processKey                      uintptr
	commitComposition               uintptr
	clearComposition                uintptr
	getCommit                       uintptr
	freeCommit                      uintptr
	getContext                      uintptr
	freeContext                     uintptr
	getStatus                       uintptr
	freeStatus                      uintptr
	setOption                       uintptr
	getOption                       uintptr
	setProperty                     uintptr
	getProperty                     uintptr
	getSchemaList                   uintptr
	freeSchemaList                  uintptr
	getCurrentSchema                uintptr
	selectSchema                    uintptr
	schemaOpen                      uintptr
	configOpen                      uintptr
	configClose                     uintptr
	configGetBool                   uintptr
	configGetInt                    uintptr
	configGetDouble                 uintptr
	configGetString                 uintptr
	configGetCString                uintptr
	configUpdateSignature           uintptr
	configBeginMap                  uintptr
	configNext                      uintptr
	configEnd                       uintptr
	simulateKeySequence             uintptr
	registerModule                  uintptr
	findModule                      uintptr
	runTask                         uintptr
	getSharedDataDir                uintptr
	getUserDataDir                  uintptr
	getSyncDir                      uintptr
	getUserID                       uintptr
	getUserDataSyncDir              uintptr
	configInit                      uintptr
	configLoadString                uintptr
	configSetBool                   uintptr
	configSetInt                    uintptr
	configSetDouble                 uintptr
	configSetString                 uintptr
	configGetItem                   uintptr
	configSetItem                   uintptr
	configClear                     uintptr
	configCreateList                uintptr
	configCreateMap                 uintptr
	configListSize                  uintptr
	configBeginList                 uintptr
	getInput                        uintptr
	getCaretPos                     uintptr
	selectCandidate                 uintptr
	getVersion                      uintptr
	setCaretPos                     uintptr
	selectCandidateOnCurrentPage    uintptr
	candidateListBegin              uintptr
	candidateListNext               uintptr
	candidateListEnd                uintptr
	userConfigOpen                  uintptr
	candidateListFromIndex          uintptr
	getPrebuiltDataDir              uintptr
	getStagingDir                   uintptr
	commitProto                     uintptr
	contextProto                    uintptr
	statusProto                     uintptr
	getStateLabel                   uintptr
	deleteCandidate                 uintptr
	deleteCandidateOnCurrentPage    uintptr
	getStateLabelAbbreviated        uintptr
	setInput                        uintptr
	getSharedDataDirS               uintptr
	getUserDataDirS                 uintptr
	getPrebuiltDataDirS             uintptr
	getStagingDirS                  uintptr
	getSyncDirS                     uintptr
	highlightCandidate              uintptr
	highlightCandidateOnCurrentPage uintptr
	changePage                      uintptr
}
