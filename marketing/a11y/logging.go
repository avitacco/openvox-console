package main

import (
	"log"
	"strings"
)

// quietErrorf filters chromedp's internal error log.
//
// Its generated protocol definitions trail the browser: Chrome 131 sends
// network events carrying IPAddressSpace values the pinned cdproto does
// not know, and each one logs an unmarshalling error. There are hundreds
// per page, and they bury the audit report. Only that known-benign class
// is dropped; anything else still prints.
func quietErrorf(format string, args ...any) {
	if strings.Contains(format, "could not unmarshal event") {
		return
	}
	log.Printf(format, args...)
}
