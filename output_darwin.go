package main

/*
#cgo LDFLAGS: -framework CoreAudio -framework CoreFoundation
#include <CoreAudio/CoreAudio.h>
#include <CoreFoundation/CoreFoundation.h>
#include <string.h>

// Read and set the system output device. taal needs this because picking a
// capture source is only half the job: the mac also has to be sending its
// audio into the loopback device, and that is a separate setting the user
// would otherwise have to change by hand in Audio MIDI Setup.

static AudioDeviceID defaultOutput(void) {
    AudioDeviceID dev = kAudioObjectUnknown;
    UInt32 size = sizeof(dev);
    AudioObjectPropertyAddress addr = {
        kAudioHardwarePropertyDefaultOutputDevice,
        kAudioObjectPropertyScopeGlobal,
        kAudioObjectPropertyElementMain,
    };
    AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, 0, NULL, &size, &dev);
    return dev;
}

static int setDefaultOutput(AudioDeviceID dev) {
    AudioObjectPropertyAddress addr = {
        kAudioHardwarePropertyDefaultOutputDevice,
        kAudioObjectPropertyScopeGlobal,
        kAudioObjectPropertyElementMain,
    };
    return AudioObjectSetPropertyData(kAudioObjectSystemObject, &addr, 0, NULL,
                                      sizeof(dev), &dev) == noErr;
}

static int deviceCount(void) {
    UInt32 size = 0;
    AudioObjectPropertyAddress addr = {
        kAudioHardwarePropertyDevices,
        kAudioObjectPropertyScopeGlobal,
        kAudioObjectPropertyElementMain,
    };
    if (AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &addr, 0, NULL, &size) != noErr)
        return 0;
    return size / sizeof(AudioDeviceID);
}

static AudioDeviceID deviceAt(int i) {
    UInt32 size = 0;
    AudioObjectPropertyAddress addr = {
        kAudioHardwarePropertyDevices,
        kAudioObjectPropertyScopeGlobal,
        kAudioObjectPropertyElementMain,
    };
    if (AudioObjectGetPropertyDataSize(kAudioObjectSystemObject, &addr, 0, NULL, &size) != noErr)
        return kAudioObjectUnknown;
    int n = size / sizeof(AudioDeviceID);
    if (i < 0 || i >= n) return kAudioObjectUnknown;
    AudioDeviceID *ids = malloc(size);
    AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, 0, NULL, &size, ids);
    AudioDeviceID d = ids[i];
    free(ids);
    return d;
}

static int deviceName(AudioDeviceID dev, char *buf, int cap) {
    CFStringRef name = NULL;
    UInt32 size = sizeof(name);
    AudioObjectPropertyAddress addr = {
        kAudioObjectPropertyName,
        kAudioObjectPropertyScopeGlobal,
        kAudioObjectPropertyElementMain,
    };
    if (AudioObjectGetPropertyData(dev, &addr, 0, NULL, &size, &name) != noErr || !name)
        return 0;
    int ok = CFStringGetCString(name, buf, cap, kCFStringEncodingUTF8);
    CFRelease(name);
    return ok;
}

// a device with no output streams is an input, not somewhere audio can go
static int hasOutput(AudioDeviceID dev) {
    UInt32 size = 0;
    AudioObjectPropertyAddress addr = {
        kAudioDevicePropertyStreamConfiguration,
        kAudioDevicePropertyScopeOutput,
        kAudioObjectPropertyElementMain,
    };
    if (AudioObjectGetPropertyDataSize(dev, &addr, 0, NULL, &size) != noErr || size == 0)
        return 0;
    AudioBufferList *bl = malloc(size);
    int channels = 0;
    if (AudioObjectGetPropertyData(dev, &addr, 0, NULL, &size, bl) == noErr) {
        for (UInt32 i = 0; i < bl->mNumberBuffers; i++)
            channels += bl->mBuffers[i].mNumberChannels;
    }
    free(bl);
    return channels > 0;
}
*/
import "C"

import "unsafe"

type outputDevice struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
	Feeds   bool   `json:"feeds"` // routes into the capture source
}

func currentOutputName() string {
	dev := C.defaultOutput()
	if dev == C.kAudioObjectUnknown {
		return ""
	}
	return nameOf(dev)
}

func nameOf(dev C.AudioDeviceID) string {
	buf := make([]C.char, 256)
	if C.deviceName(dev, &buf[0], 256) == 0 {
		return ""
	}
	return C.GoString(&buf[0])
}

func outputDevices() []outputDevice {
	current := currentOutputName()
	var out []outputDevice
	for i := 0; i < int(C.deviceCount()); i++ {
		dev := C.deviceAt(C.int(i))
		if dev == C.kAudioObjectUnknown || C.hasOutput(dev) == 0 {
			continue
		}
		name := nameOf(dev)
		if name == "" {
			continue
		}
		out = append(out, outputDevice{
			Name:    name,
			Current: name == current,
			Feeds:   isLoopback(name) || isAggregate(name),
		})
	}
	return out
}

// device id by name, for the routing builder
func findDeviceID(name string) (C.AudioDeviceID, bool) {
	for i := 0; i < int(C.deviceCount()); i++ {
		dev := C.deviceAt(C.int(i))
		if dev == C.kAudioObjectUnknown {
			continue
		}
		if nameOf(dev) == name {
			return dev, true
		}
	}
	return 0, false
}

func setOutput(name string) bool {
	for i := 0; i < int(C.deviceCount()); i++ {
		dev := C.deviceAt(C.int(i))
		if dev == C.kAudioObjectUnknown || C.hasOutput(dev) == 0 {
			continue
		}
		if nameOf(dev) == name {
			return C.setDefaultOutput(dev) != 0
		}
	}
	return false
}

var _ = unsafe.Pointer(nil)
