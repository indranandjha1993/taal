package main

import "os/exec"

// Everything needed to go from "a mac with music playing" to "a mac whose
// audio taal can hear", without the person running it knowing what a
// loopback driver is. The host page shows one button; this decides what
// that button has to do.

type audioSetup struct {
	Ready     bool   `json:"ready"`     // taal can capture right now
	Loopback  string `json:"loopback"`  // the driver it found, if any
	Speakers  string `json:"speakers"`  // real speakers to keep audible
	Installer string `json:"installer"` // how to fix it, when not ready
	Detail    string `json:"detail"`    // one line for a person to read
}

// what the machine looks like before taal touches anything
func inspectAudio(c *capture) audioSetup {
	var s audioSetup

	devs, err := c.devices()
	if err != nil {
		s.Detail = "cannot read the audio devices on this mac"
		return s
	}
	for _, d := range devs {
		if d.Loop {
			s.Loopback = d.Name
			break
		}
	}

	s.Speakers = realSpeakers()

	if s.Loopback == "" {
		s.Detail = "macos does not let an app hear its own audio without a " +
			"small helper driver. taal can install it."
		if hasBrew() {
			s.Installer = "brew"
		} else {
			s.Installer = "manual"
		}
		return s
	}

	s.Ready = true
	s.Detail = "ready to stream"
	return s
}

// the device a person would call "the speakers": a real output that is not
// a loopback and not something taal or anyone else assembled
func realSpeakers() string {
	best := ""
	for _, d := range outputDevices() {
		if isLoopback(d.Name) || isAggregate(d.Name) || d.Name == routingName {
			continue
		}
		// prefer whatever is currently selected, it is what they hear now
		if d.Current {
			return d.Name
		}
		if best == "" {
			best = d.Name
		}
	}
	return best
}

func hasBrew() bool {
	_, err := exec.LookPath("brew")
	return err == nil
}

func installCommand() string {
	return "brew install --cask blackhole-2ch"
}

// BlackHole is a system audio driver, so installing it needs an admin
// password. A browser request has no terminal to type one into, which means
// this can only ever hand the command over. Pretending otherwise would hang
// on a password prompt nobody can see.
func runInstall() (string, bool) {
	if !hasBrew() {
		return "homebrew is not installed. install it from brew.sh first, " +
			"or get BlackHole from existential.audio/blackhole", false
	}
	return "installing an audio driver needs an admin password, which a web " +
		"page cannot ask for. run this in a terminal:", false
}
