package rerank

// Request asks for an order of one export (ScoreRequest, DESIGN §7.2).
type Request struct {
	// Session is the session's slot. The answer echoes it and ExportSeq.
	Session   int
	ExportSeq uint32
	// App is the context slot of the input context's app, and AppRev the
	// revision of its text the order should use.
	App    int
	AppRev uint32
	// Export holds the candidates. It is valid only during Scorer.Score.
	Export *Slot
}

// Scorer is the engine's link to the model (shixin-ai). Its methods run on
// the engine thread: they must not block, and must not keep their
// arguments. Answers go to the engine's Rerank method.
type Scorer interface {
	// Context reports an app's committed text after it changed.
	Context(app int, rev uint32, text []byte)
	// Score asks for an order of an export.
	Score(r *Request)
	// Ready reports whether an order can come back: the engine holds a
	// key's result for one only then.
	Ready() bool
}
