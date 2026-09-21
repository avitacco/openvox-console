package main

import (
	"log"
	"strings"
)

// quietErrorf filters chromedp's internal error log.
//
// chromedp decodes every DevTools event into typed structs, and its
// generated protocol definitions trail the browser: Chrome 131 sends
// network events carrying IPAddressSpace values ("Private", "Public")
// that the pinned cdproto does not know, and each one logs an
// unmarshalling error. They are harmless - the events are telemetry this
// tool never reads - but there are hundreds per page, and they bury the
// one message that matters when a capture fails.
//
// Only that known-benign class is dropped. Anything else still prints,
// because an error this tool does not recognise is exactly the kind it
// should not be hiding.
func quietErrorf(format string, args ...any) {
	if strings.Contains(format, "could not unmarshal event") {
		return
	}
	log.Printf(format, args...)
}
