package main

/*
#cgo LDFLAGS: -framework CoreAudio -framework CoreFoundation
#include <CoreAudio/CoreAudio.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>

// taal builds its own output device rather than asking people to assemble
// one in Audio MIDI Setup. A stacked aggregate plays the same audio into
// every member at once, so putting the real speakers and the loopback
// driver in one means the mac keeps making sound while taal can hear it.

static CFStringRef uidOf(AudioDeviceID dev) {
    CFStringRef uid = NULL;
    UInt32 size = sizeof(uid);
    AudioObjectPropertyAddress addr = {
        kAudioDevicePropertyDeviceUID,
        kAudioObjectPropertyScopeGlobal,
        kAudioObjectPropertyElementMain,
    };
    if (AudioObjectGetPropertyData(dev, &addr, 0, NULL, &size, &uid) != noErr) return NULL;
    return uid;
}

static AudioDeviceID makeRouting(CFStringRef loopUID, CFStringRef spkUID,
                                 CFStringRef name, CFStringRef uid) {
    CFMutableDictionaryRef desc = CFDictionaryCreateMutable(NULL, 0,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFDictionarySetValue(desc, CFSTR(kAudioAggregateDeviceNameKey), name);
    CFDictionarySetValue(desc, CFSTR(kAudioAggregateDeviceUIDKey), uid);

    int one = 1;
    CFNumberRef yes = CFNumberCreate(NULL, kCFNumberIntType, &one);
    // stacked means multi output: every member gets the same audio
    CFDictionarySetValue(desc, CFSTR(kAudioAggregateDeviceIsStackedKey), yes);

    CFMutableArrayRef subs = CFArrayCreateMutable(NULL, 0, &kCFTypeArrayCallBacks);
    // the loopback goes first and is the clock master. it is the one device
    // guaranteed present, and drift correction then applies to the others.
    CFStringRef members[2] = { loopUID, spkUID };
    int count = spkUID ? 2 : 1;
    for (int i = 0; i < count; i++) {
        CFMutableDictionaryRef sub = CFDictionaryCreateMutable(NULL, 0,
            &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
        CFDictionarySetValue(sub, CFSTR(kAudioSubDeviceUIDKey), members[i]);
        if (i > 0) {
            CFNumberRef drift = CFNumberCreate(NULL, kCFNumberIntType, &one);
            CFDictionarySetValue(sub, CFSTR(kAudioSubDeviceDriftCompensationKey), drift);
            CFRelease(drift);
        }
        CFArrayAppendValue(subs, sub);
        CFRelease(sub);
    }
    CFDictionarySetValue(desc, CFSTR(kAudioAggregateDeviceSubDeviceListKey), subs);
    CFDictionarySetValue(desc, CFSTR(kAudioAggregateDeviceMasterSubDeviceKey), loopUID);

    AudioDeviceID out = kAudioObjectUnknown;
    OSStatus err = AudioHardwareCreateAggregateDevice(desc, &out);
    CFRelease(subs); CFRelease(yes); CFRelease(desc);
    if (err != noErr) return kAudioObjectUnknown;
    return out;
}

static int destroyRouting(AudioDeviceID d) {
    return AudioHardwareDestroyAggregateDevice(d) == noErr;
}
*/
import "C"

import "unsafe"

// the device taal builds. the uid is fixed so a leftover from a crashed run
// is found and reused rather than piling up duplicates.
const (
	routingName = "taal output"
	routingUID  = "space.injha.taal.output"
)

func cfstr(s string) C.CFStringRef {
	c := C.CString(s)
	defer C.free(unsafe.Pointer(c))
	return C.CFStringCreateWithCString(0, c, C.kCFStringEncodingUTF8)
}

// build (or find) the device that plays to the speakers and the loopback at
// once. speakers may be empty, in which case only the loopback is included
// and the mac itself stays silent.
func buildRouting(loopback, speakers string) (string, bool) {
	if dev, ok := findDeviceID(routingName); ok {
		C.destroyRouting(dev)
	}

	loopID, ok := findDeviceID(loopback)
	if !ok {
		return "", false
	}
	loopUID := C.uidOf(loopID)
	if loopUID == 0 {
		return "", false
	}
	defer C.CFRelease(C.CFTypeRef(loopUID))

	var spkUID C.CFStringRef
	if speakers != "" {
		if spkID, ok := findDeviceID(speakers); ok {
			spkUID = C.uidOf(spkID)
			if spkUID != 0 {
				defer C.CFRelease(C.CFTypeRef(spkUID))
			}
		}
	}

	name := cfstr(routingName)
	uid := cfstr(routingUID)
	defer C.CFRelease(C.CFTypeRef(name))
	defer C.CFRelease(C.CFTypeRef(uid))

	dev := C.makeRouting(loopUID, spkUID, name, uid)
	if dev == C.kAudioObjectUnknown {
		return "", false
	}
	return routingName, true
}

func removeRouting() {
	if dev, ok := findDeviceID(routingName); ok {
		C.destroyRouting(dev)
	}
}
