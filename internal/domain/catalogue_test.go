package domain

import (
	"reflect"
	"testing"
)

// The catalogue XML schema and the minBin 64-bit minute-bitmask semantics are
// taken from the decompiled SAX handler C3039OooO0o0 (doc §12.6): <video> >
// <catalogue date hNumb> > <option hours minBin>, where minBin bit i set means
// minute i of that hour has a recording.
func TestParseCatalogue_NestedStructure(t *testing.T) {
	xml := []byte(`<video>
		<catalogue date="20260805" hNumb="2">
			<option hours="14" minBin="6"/>
			<option hours="9" minBin="1"/>
		</catalogue>
		<catalogue date="20260804" hNumb="1">
			<option hours="0" minBin="0"/>
		</catalogue>
	</video>`)

	got, err := ParseCatalogue(xml)
	if err != nil {
		t.Fatalf("ParseCatalogue error: %v", err)
	}

	want := Catalogue{Days: []CatalogueDay{
		{Date: "20260805", Hours: []CatalogueHour{
			{Hour: 14, Minutes: []int{1, 2}}, // minBin 6 = 0b110 → minutes 1,2
			{Hour: 9, Minutes: []int{0}},     // minBin 1 = bit 0 → minute 0
		}},
		{Date: "20260804", Hours: []CatalogueHour{
			{Hour: 0, Minutes: nil}, // minBin 0 → no minutes
		}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseCatalogue =\n%#v\nwant\n%#v", got, want)
	}
}

// The bitmask spans the full 0–59 minute range as a 64-bit value.
func TestParseCatalogue_BitmaskSpansFullMinuteRange(t *testing.T) {
	// bit 0 + bit 59 = 1 + (1<<59) = 576460752303423489
	xml := []byte(`<video><catalogue date="20260805" hNumb="1"><option hours="23" minBin="576460752303423489"/></catalogue></video>`)
	got, err := ParseCatalogue(xml)
	if err != nil {
		t.Fatalf("ParseCatalogue error: %v", err)
	}
	if len(got.Days) != 1 || len(got.Days[0].Hours) != 1 {
		t.Fatalf("unexpected shape: %#v", got)
	}
	if want := []int{0, 59}; !reflect.DeepEqual(got.Days[0].Hours[0].Minutes, want) {
		t.Fatalf("minutes = %v, want %v", got.Days[0].Hours[0].Minutes, want)
	}
}

func TestParseCatalogue_Empty(t *testing.T) {
	got, err := ParseCatalogue([]byte(`<video></video>`))
	if err != nil {
		t.Fatalf("ParseCatalogue error: %v", err)
	}
	if len(got.Days) != 0 {
		t.Fatalf("Days = %#v, want empty", got.Days)
	}
}

// Lenient like the source: an unparseable minBin yields an hour with no minutes
// rather than failing the whole parse.
func TestParseCatalogue_MalformedMinBinIsLenient(t *testing.T) {
	xml := []byte(`<video><catalogue date="20260805" hNumb="1"><option hours="12" minBin="notanumber"/></catalogue></video>`)
	got, err := ParseCatalogue(xml)
	if err != nil {
		t.Fatalf("ParseCatalogue error: %v", err)
	}
	if len(got.Days) != 1 || len(got.Days[0].Hours) != 1 {
		t.Fatalf("unexpected shape: %#v", got)
	}
	if h := got.Days[0].Hours[0]; h.Hour != 12 || len(h.Minutes) != 0 {
		t.Fatalf("hour = %#v, want hour 12 with no minutes", h)
	}
}

// Options whose hours attribute isn't a valid hour are skipped, keeping the
// valid one. This parser requires all-digit, length ≤ 2, and range 0–23 — so it
// rejects out-of-range ("99"), signed ("+1"), non-digit ("1x"), and over-long
// ("999") hours (stricter than the app, which only rejects length > 2 / non-digit
// — see doc §12.6).
func TestParseCatalogue_SkipsBadHours(t *testing.T) {
	for _, bad := range []string{"99", "+1", "-1", "1x", "999", " 1", ""} {
		xml := []byte(`<video><catalogue date="20260805" hNumb="2"><option hours="` + bad + `" minBin="1"/><option hours="8" minBin="1"/></catalogue></video>`)
		got, err := ParseCatalogue(xml)
		if err != nil {
			t.Fatalf("hours=%q: ParseCatalogue error: %v", bad, err)
		}
		if len(got.Days) != 1 {
			t.Fatalf("hours=%q: Days = %#v", bad, got.Days)
		}
		if hs := got.Days[0].Hours; len(hs) != 1 || hs[0].Hour != 8 {
			t.Fatalf("hours=%q: Hours = %#v, want only hour 8", bad, hs)
		}
	}
}

// Duplicate <catalogue> dates merge into one day; a duplicate hour within a day
// is first-wins — mirroring the source's map-keyed accumulation.
func TestParseCatalogue_MergesDuplicateDatesAndHours(t *testing.T) {
	xml := []byte(`<video>` +
		`<catalogue date="20260805" hNumb="1"><option hours="14" minBin="1"/></catalogue>` +
		`<catalogue date="20260805" hNumb="1"><option hours="15" minBin="2"/><option hours="14" minBin="6"/></catalogue>` +
		`</video>`)
	got, err := ParseCatalogue(xml)
	if err != nil {
		t.Fatalf("ParseCatalogue error: %v", err)
	}
	if len(got.Days) != 1 {
		t.Fatalf("Days = %#v, want a single merged day", got.Days)
	}
	want := []CatalogueHour{
		{Hour: 14, Minutes: []int{0}}, // first occurrence (minBin 1) wins over the later minBin 6
		{Hour: 15, Minutes: []int{1}},
	}
	if !reflect.DeepEqual(got.Days[0].Hours, want) {
		t.Fatalf("Hours = %#v, want %#v", got.Days[0].Hours, want)
	}
}

// A negative minBin (which the app's signed parse would accept but our unsigned
// parse rejects) is treated leniently as no minutes, not a fatal error.
func TestParseCatalogue_NegativeMinBinIsLenient(t *testing.T) {
	xml := []byte(`<video><catalogue date="20260805" hNumb="1"><option hours="3" minBin="-1"/></catalogue></video>`)
	got, err := ParseCatalogue(xml)
	if err != nil {
		t.Fatalf("ParseCatalogue error: %v", err)
	}
	if len(got.Days) != 1 || len(got.Days[0].Hours) != 1 || len(got.Days[0].Hours[0].Minutes) != 0 {
		t.Fatalf("unexpected: %#v", got)
	}
}

// The reassembled payload may carry an XML declaration and surrounding
// whitespace; both must parse fine.
func TestParseCatalogue_XMLDeclarationPrefix(t *testing.T) {
	xml := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<video><catalogue date=\"20260805\" hNumb=\"1\"><option hours=\"7\" minBin=\"1\"/></catalogue></video>")
	got, err := ParseCatalogue(xml)
	if err != nil {
		t.Fatalf("ParseCatalogue error: %v", err)
	}
	if len(got.Days) != 1 || got.Days[0].Date != "20260805" {
		t.Fatalf("unexpected: %#v", got)
	}
}

func TestParseCatalogue_MalformedXMLErrors(t *testing.T) {
	if _, err := ParseCatalogue([]byte(`<video><catalogue`)); err == nil {
		t.Fatal("ParseCatalogue accepted malformed XML")
	}
}
