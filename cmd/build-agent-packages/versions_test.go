package main

import (
	"strings"
	"testing"
	"time"
	"unicode"
)

// debOrder and rpmOrder below are the two package managers' own version
// comparison rules (deb-version(7); rpm's rpmvercmp), written out so the
// ordering promise can be checked on a build machine that has neither
// dpkg nor rpm.

func debCharOrder(c byte) int {
	switch {
	case c == '~':
		return -1
	case c == 0:
		return 0
	case unicode.IsLetter(rune(c)):
		return int(c)
	default:
		return int(c) + 256
	}
}

func debPartCompare(a, b string) int {
	for a != "" || b != "" {
		// Non-digit prefix, compared character by character.
		for (a != "" && !isDigit(a[0])) || (b != "" && !isDigit(b[0])) {
			var ca, cb byte
			if a != "" && !isDigit(a[0]) {
				ca = a[0]
				a = a[1:]
			}
			if b != "" && !isDigit(b[0]) {
				cb = b[0]
				b = b[1:]
			}
			if d := debCharOrder(ca) - debCharOrder(cb); d != 0 {
				return d
			}
		}
		na, ra := takeDigits(a)
		nb, rb := takeDigits(b)
		a, b = ra, rb
		if d := numCompare(na, nb); d != 0 {
			return d
		}
	}
	return 0
}

// debCompare orders two Debian versions with no epoch.
func debCompare(a, b string) int {
	split := func(v string) (string, string) {
		if i := strings.LastIndex(v, "-"); i >= 0 {
			return v[:i], v[i+1:]
		}
		return v, ""
	}
	ua, ra := split(a)
	ub, rb := split(b)
	if d := debPartCompare(ua, ub); d != 0 {
		return d
	}
	return debPartCompare(ra, rb)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func takeDigits(s string) (digits, rest string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i], s[i:]
}

func numCompare(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

// rpmvercmp orders two rpm version (or release) strings.
func rpmvercmp(a, b string) int {
	isAlnum := func(c byte) bool { return isDigit(c) || unicode.IsLetter(rune(c)) }
	for a != "" || b != "" {
		for a != "" && !isAlnum(a[0]) {
			a = a[1:]
		}
		for b != "" && !isAlnum(b[0]) {
			b = b[1:]
		}
		if a == "" || b == "" {
			break
		}
		var sa, sb string
		numeric := isDigit(a[0])
		take := func(s string) (string, string) {
			i := 0
			for i < len(s) && isAlnum(s[i]) && isDigit(s[i]) == numeric {
				i++
			}
			return s[:i], s[i:]
		}
		sa, a = take(a)
		sb, b = take(b)
		if sb == "" {
			// Different segment types: numeric is newer than alphabetic.
			if numeric {
				return 1
			}
			return -1
		}
		var d int
		if numeric {
			d = numCompare(sa, sb)
		} else {
			d = strings.Compare(sa, sb)
		}
		if d != 0 {
			return d
		}
	}
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return -1
	default:
		return 1
	}
}

// rpmCompare orders two epoch:version-release triples.
func rpmCompare(epochA, verA, relA, epochB, verB, relB string) int {
	if epochA == "" {
		epochA = "0"
	}
	if epochB == "" {
		epochB = "0"
	}
	if d := numCompare(epochA, epochB); d != 0 {
		return d
	}
	if d := rpmvercmp(verA, verB); d != 0 {
		return d
	}
	return rpmvercmp(relA, relB)
}

func TestComparators(t *testing.T) {
	// A few known answers, so a mistake in the comparators above cannot
	// quietly make the real assertions pass.
	if debCompare("1.0~rc1", "1.0") >= 0 || debCompare("1.10", "1.9") <= 0 || debCompare("1.0-1", "1.0-2") >= 0 {
		t.Fatal("debCompare disagrees with dpkg on known cases")
	}
	if rpmvercmp("1.10", "1.9") <= 0 || rpmvercmp("1.0", "1.0a") >= 0 || rpmvercmp("5", "abc") <= 0 || rpmvercmp("4a4a7ef", "0.1.0") <= 0 {
		t.Fatal("rpmvercmp disagrees with rpm on known cases")
	}
}

func TestPackageVersions(t *testing.T) {
	built := time.Date(2026, 11, 11, 9, 30, 0, 0, time.UTC)

	deb, ver, rel := packageVersions("bbba663-dirty", built)
	if deb != "0.1.0+20261111093000.bbba663.dirty" || ver != "0.1.0" || rel != "20261111093000.bbba663_dirty" {
		t.Errorf("untagged: got deb %q, rpm %q-%q", deb, ver, rel)
	}

	deb, ver, rel = packageVersions("v0.3.2-14-g1a2b3c4", built)
	if deb != "0.3.2+20261111093000.14.g1a2b3c4" || ver != "0.3.2" || rel != "20261111093000.14_g1a2b3c4" {
		t.Errorf("tagged: got deb %q, rpm %q-%q", deb, ver, rel)
	}

	deb, ver, rel = packageVersions("v1.0.0", built)
	if deb != "1.0.0+20261111093000" || ver != "1.0.0" || rel != "20261111093000" {
		t.Errorf("exact tag: got deb %q, rpm %q-%q", deb, ver, rel)
	}

	deb, _, _ = packageVersions("dev", built)
	if deb != "0.1.0+20261111093000.dev" {
		t.Errorf("dev: got deb %q", deb)
	}
}

// Every version this project served before versions were ordered: deb
// was 0.0.0+<describe>, rpm was <describe with underscores>-1 with no
// epoch. Shas are listed both letter- and digit-leading, since rpm
// treats the two very differently.
var shippedDescribes = []string{"bbba663-dirty", "bbba663", "4a4a7ef", "4a4a7ef-dirty", "9999999", "d2999e8", "dev"}

func TestNewPackagesUpgradeEveryShippedVersion(t *testing.T) {
	deb, ver, rel := packageVersions("0000001", time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC))
	for _, old := range shippedDescribes {
		oldDeb := "0.0.0+" + old
		if debCompare(deb, oldDeb) <= 0 {
			t.Errorf("deb %s does not upgrade shipped %s", deb, oldDeb)
		}
		oldRPM := strings.ReplaceAll(old, "-", "_")
		if rpmCompare(rpmEpoch, ver, rel, "", oldRPM, "1") <= 0 {
			t.Errorf("rpm %s:%s-%s does not upgrade shipped %s-1", rpmEpoch, ver, rel, oldRPM)
		}
	}
}

func TestLaterBuildOrdersAfterEarlier(t *testing.T) {
	early := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	late := early.Add(time.Hour)

	// The same revision rebuilt later, and a later build whose revision
	// sorts lower: build time has to win both.
	for _, c := range []struct{ earlyDescribe, lateDescribe string }{
		{"bbba663", "bbba663"},
		{"fffffff", "0000000"},
		{"9999999-dirty", "aaaaaaa"},
		{"v0.3.2-14-g1a2b3c4", "v0.3.2-2-g0000000"},
	} {
		debE, verE, relE := packageVersions(c.earlyDescribe, early)
		debL, verL, relL := packageVersions(c.lateDescribe, late)
		if debCompare(debL, debE) <= 0 {
			t.Errorf("deb %s (later) does not order after %s", debL, debE)
		}
		if rpmCompare(rpmEpoch, verL, relL, rpmEpoch, verE, relE) <= 0 {
			t.Errorf("rpm %s-%s (later) does not order after %s-%s", verL, relL, verE, relE)
		}
	}
}
