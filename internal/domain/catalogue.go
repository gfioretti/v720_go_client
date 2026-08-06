package domain

import (
	"encoding/xml"
	"fmt"
	"strconv"
)

// Catalogue is the SD-card recording index delivered as the type-8 XML payload
// (findings doc §12.6): which date/hour/minute recordings exist on the card. A
// caller uses it to pick a date+hour+minute to fetch or play back via the 404/
// 406 commands.
type Catalogue struct {
	Days []CatalogueDay
}

// CatalogueDay is one date's recordings (Date is the raw catalogue date string,
// e.g. "20260805").
type CatalogueDay struct {
	Date  string
	Hours []CatalogueHour
}

// CatalogueHour lists the minutes (0–59, ascending) within Hour (0–23) that have
// a recording.
type CatalogueHour struct {
	Hour    int
	Minutes []int
}

// XML wire shape (SAX handler C3039OooO0o0): <video> > <catalogue date hNumb> >
// <option hours minBin>. hNumb (an hour count used only as a map-capacity hint
// by the app) is not needed here.
type xmlVideo struct {
	XMLName    xml.Name       `xml:"video"`
	Catalogues []xmlCatalogue `xml:"catalogue"`
}

type xmlCatalogue struct {
	Date    string      `xml:"date,attr"`
	Options []xmlOption `xml:"option"`
}

type xmlOption struct {
	Hours  string `xml:"hours,attr"`
	MinBin string `xml:"minBin,attr"`
}

// ParseCatalogue parses the reassembled type-8 catalogue XML into a Catalogue.
//
// A malformed XML document is an error; individual malformed entries are handled
// leniently, mirroring the app's SAX handler: an option whose `hours` is not a
// valid hour is skipped, and an unparseable `minBin` yields an hour with no
// minutes — neither fails the whole parse. Following the source's map-keyed
// accumulation, duplicate `<catalogue>` dates are merged into one day and a
// duplicate hour within a day is first-wins.
func ParseCatalogue(data []byte) (Catalogue, error) {
	var doc xmlVideo
	if err := xml.Unmarshal(data, &doc); err != nil {
		return Catalogue{}, fmt.Errorf("domain: parse catalogue XML: %w", err)
	}

	cat := Catalogue{}
	dayIndex := make(map[string]int)      // date -> index into cat.Days (merge dup dates)
	seenHour := make(map[string]struct{}) // "dayIdx:hour" -> present (first-wins per hour)

	for _, c := range doc.Catalogues {
		di, ok := dayIndex[c.Date]
		if !ok {
			cat.Days = append(cat.Days, CatalogueDay{Date: c.Date})
			di = len(cat.Days) - 1
			dayIndex[c.Date] = di
		}
		for _, o := range c.Options {
			hour, ok := parseHour(o.Hours)
			if !ok {
				continue
			}
			hourKey := strconv.Itoa(di) + ":" + strconv.Itoa(hour)
			if _, dup := seenHour[hourKey]; dup {
				continue
			}
			seenHour[hourKey] = struct{}{}
			cat.Days[di].Hours = append(cat.Days[di].Hours, CatalogueHour{
				Hour:    hour,
				Minutes: minutesFromBitmask(o.MinBin),
			})
		}
	}
	return cat, nil
}

// parseHour validates an `hours` attribute and returns the hour. It requires an
// all-ASCII-digit string of length 1–2 (mirroring the source's isDigit check,
// which rejects the signs/whitespace that strconv.Atoi would otherwise accept)
// denoting a real hour 0–23. Anything else is skipped. (The source does not
// range-check 24–99, but those are not valid hours.)
func parseHour(s string) (int, bool) {
	if len(s) < 1 || len(s) > 2 || !allDigits(s) {
		return 0, false
	}
	h, _ := strconv.Atoi(s) // safe: all digits, length ≤ 2
	if h > 23 {
		return 0, false
	}
	return h, true
}

func allDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// minutesFromBitmask decodes the `minBin` attribute — a decimal 64-bit value in
// which bit i set means minute i has a recording — into an ascending minute list
// (minutes 0–59). An unparseable value yields no minutes (matching the source's
// NumberFormatException path).
//
// This is deliberately MORE correct than the official app: the app's SAX handler
// scans the mask with 32-bit `1 << i` shifts (not `1L << i`), so the shift wraps
// mod 32 and it is blind to minutes ~31–59 (doc §12.6 discrepancy note). The
// camera sets the full 64-bit mask, so we decode all of bits 0–59.
//
// ParseUint (unsigned) differs from the app's signed Long.parseLong only on
// hostile input (negative or > int64max values); for real masks (< 2^60) they
// agree, and either way the result is bounded to minutes 0–59.
func minutesFromBitmask(minBin string) []int {
	mask, err := strconv.ParseUint(minBin, 10, 64)
	if err != nil {
		return nil
	}
	var minutes []int
	for i := range 60 {
		if mask&(uint64(1)<<i) != 0 {
			minutes = append(minutes, i)
		}
	}
	return minutes
}
