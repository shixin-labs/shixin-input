// Command shixin-ai scores candidates for 智能排序 (DESIGN §7.2). It holds
// the language model on the GPU through llama.cpp and answers the engine
// over a Unix socket in Shixin's 0700 runtime directory. It talks to no
// client, opens no network sockets and never sees the user dictionary.
//
// Exit status 3 means there is no supported accelerator: the model never
// runs on the CPU. Status 4 means the model file is missing. Neither is
// worth a restart.
package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"

	"shixin/internal/ai"
	"shixin/internal/ipc"
	"shixin/internal/llama"
	"shixin/internal/logx"
	"shixin/internal/models"
	"shixin/internal/rerank"
)

func main() {
	lib := flag.String("llama", defaultLlama(), "directory with libllama and its libggml*")
	model := flag.String("model", defaultModel(), "GGUF model")
	socket := flag.String("socket", "", "socket to listen on (default: ai.sock in the runtime dir)")
	logPath := flag.String("log", logx.DefaultPath("ai.log"), "log path (- for stderr)")
	lifeline := flag.Bool("lifeline", false, "exit when stdin closes (run by shixin-engine)")
	flag.Parse()
	if *lifeline {
		go func() {
			io.Copy(io.Discard, os.Stdin)
			os.Exit(0)
		}()
	}

	logger := logx.New(os.Stderr)
	if *logPath != "" && *logPath != "-" {
		if l, err := logx.Open(*logPath); err == nil {
			logger = l
		}
	}
	code := run(*lib, *model, *socket, logger.Printf)
	logger.Close()
	os.Exit(code)
}

func run(libDir, model, socket string, logf func(string, ...any)) int {
	// The model's weights live in llama.cpp's memory, not Go's.
	debug.SetGCPercent(400)
	if socket == "" {
		dir, err := ipc.RuntimeDir()
		if err != nil {
			logf("ai: %v", err)
			return 1
		}
		socket = rerank.SocketPath(dir)
	}
	// Scoring is latency-sensitive: the model runs on a thread with
	// interactive QoS, locked for the process's life.
	runtime.LockOSThread()
	setInteractivePriority()
	if _, err := os.Stat(model); err != nil {
		logf("ai: model: %v", err)
		return rerank.ExitNoModel
	}
	lib, err := llama.Load(libDir)
	if err != nil {
		logf("ai: %v", err)
		return 1
	}
	s, err := ai.New(lib, model)
	if errors.Is(err, ai.ErrNoAccelerator) {
		logf("ai: %v", err)
		return rerank.ExitNoAccelerator
	}
	if err != nil {
		logf("ai: %v", err)
		return 1
	}
	defer s.Close()
	l, err := ipc.Listen(socket)
	if err != nil {
		logf("ai: %v", err)
		return 1
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		<-sig
		l.Close()
	}()
	logf("ai: ready on %s with %s", socket, filepath.Base(model))
	if err := ai.NewServer(s, logf).Serve(l); err != nil {
		logf("ai: %v", err)
		return 1
	}
	return 0
}

// defaultLlama is Contents/Frameworks in the app bundle, else the
// developer build (scripts/build-llama.sh) next to build/bin;
// SHIXIN_LLAMA overrides it.
func defaultLlama() string {
	if p := os.Getenv("SHIXIN_LLAMA"); p != "" {
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	if runtime.GOOS == "windows" {
		return dir
	}
	if p := filepath.Join(dir, "..", "Frameworks"); fileExists(filepath.Join(p, "libllama.0.dylib")) {
		return p
	}
	return filepath.Join(dir, "..", "llama", "lib")
}

// defaultModel is the model the settings app downloads (phase 3), in the
// settings directory; SHIXIN_AI_MODEL overrides it for development.
func defaultModel() string {
	if p := os.Getenv("SHIXIN_AI_MODEL"); p != "" {
		return p
	}
	dir, err := models.Dir()
	if err != nil {
		return ""
	}
	return models.ERNIE.Path(dir)
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
