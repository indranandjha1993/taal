package main

import (
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// A crash or a kill would otherwise leave the mac pointed at a device taal
// invented, with part of its audio going into a loopback nobody is reading.
// The device outlives the process, so the previous output is written to disk
// the moment it is changed and put back on the next start.

type restoreState struct {
	PrevOutput string `json:"prev_output"`
}

func restorePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	dir = filepath.Join(dir, "taal")
	os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, "restore.json")
}

func rememberOutput(name string) {
	p := restorePath()
	if p == "" {
		return
	}
	b, err := json.Marshal(restoreState{PrevOutput: name})
	if err != nil {
		return
	}
	os.WriteFile(p, b, 0o600)
}

func forgetOutput() {
	if p := restorePath(); p != "" {
		os.Remove(p)
	}
}

// Called at startup, before anything else touches the audio system. Puts the
// output back if we died holding it, and clears any device we left behind.
func recoverFromCrash() string {
	var note string

	p := restorePath()
	if p == "" {
		return note
	}
	b, err := os.ReadFile(p)
	if err == nil {
		var st restoreState
		if json.Unmarshal(b, &st) == nil && st.PrevOutput != "" {
			// only take it back if we are still the ones holding it
			if currentOutputName() == routingName {
				if setOutput(st.PrevOutput) {
					note = "put the audio output back to " + st.PrevOutput
				}
			}
		}
		os.Remove(p)
	}

	// the device itself outlives the process either way
	if _, found := findDeviceID(routingName); found {
		removeRouting()
		if note == "" {
			note = "cleaned up a leftover audio device"
		}
	}
	return note
}

// last resort on ctrl-c and friends, so the common case never needs the
// crash recovery above
func cleanupOnSignal(s *server) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		s.stopStream()
		os.Exit(0)
	}()
}
