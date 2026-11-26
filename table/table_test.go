// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package table

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func sample() *Table {
	return &Table{
		Format:      FormatVersion,
		GeneratedAt: time.Date(2026, 11, 26, 7, 0, 0, 0, time.FixedZone("JST", 9*3600)),
		NetworkID:   4,
		NITVersion:  12,
		BS: []Entry{
			{Channel: "BS01_0", Transponder: 1, Slot: 0, TSID: 0x4010, FreqKHz: 11727480,
				Services: []Service{{151, 1, "BS朝日1"}, {152, 1, "BS朝日2"}, {700, 0xc0, "データ"}}},
			{Channel: "BS15_0", Transponder: 15, Slot: 0, TSID: 0x40f1, FreqKHz: 11996000,
				Services: []Service{{101, 1, "NHK BS"}}},
			{Channel: "BS15_2", Transponder: 15, Slot: 2, TSID: 0x48f3, FreqKHz: 11996000,
				Services: []Service{{200, 1, "BS10: プレミアム"}}},
		},
	}
}

func TestParseChannel(t *testing.T) {
	for _, c := range []struct {
		in       string
		tp, slot int
		ok       bool
	}{
		{"BS15_0", 15, 0, true},
		{"bs1_2", 1, 2, true},
		{"BS01_7", 1, 7, true},
		{"BS02_0", 0, 0, false},
		{"BS25_0", 0, 0, false},
		{"BS15", 0, 0, false},
		{"BS15_8", 0, 0, false},
		{"CS2", 0, 0, false},
	} {
		tp, slot, err := ParseChannel(c.in)
		if (err == nil) != c.ok || tp != c.tp || slot != c.slot {
			t.Errorf("ParseChannel(%q) = %d, %d, %v", c.in, tp, slot, err)
		}
	}
}

func TestTransponderOfFreq(t *testing.T) {
	for tp := 1; tp <= 23; tp += 2 {
		if got := TransponderOfFreq(FreqOfTransponder(tp)); got != tp {
			t.Errorf("tp %d: got %d", tp, got)
		}
	}
	if got := TransponderOfFreq(FreqOfTransponder(3) + 19180); got != 0 {
		t.Errorf("中間の周波数で %d", got)
	}
	if got := TransponderOfFreq(12291000); got != 0 { // 110 度 CS ND2
		t.Errorf("CS の周波数で %d", got)
	}
}

func TestRoundTripAndLookup(t *testing.T) {
	var buf bytes.Buffer
	if err := sample().Write(&buf); err != nil {
		t.Fatal(err)
	}
	got, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := got.ByChannel("bs15_2"); !ok || e.TSID != 0x48f3 {
		t.Errorf("ByChannel = %+v %v", e, ok)
	}
	if e, ok := got.ByTSID(0x4010); !ok || e.Name() != "BS朝日1" {
		t.Errorf("ByTSID = %+v %v", e, ok)
	}
	if es := got.Transponder(15); len(es) != 2 || es[1].Slot != 2 {
		t.Errorf("Transponder(15) = %+v", es)
	}
	if _, err := Read(strings.NewReader(`{"format":99}`)); err == nil {
		t.Error("知らない版を読めてしまった")
	}
}

func TestWriteMirakurun(t *testing.T) {
	var buf bytes.Buffer
	if err := sample().WriteMirakurun(&buf); err != nil {
		t.Fatal(err)
	}
	want := `- name: BS朝日1
  type: BS
  channel: BS01_0
- name: NHK BS
  type: BS
  channel: BS15_0
- name: "BS10: プレミアム"
  type: BS
  channel: BS15_2
`
	got := buf.String()
	if i := strings.IndexByte(got, '\n'); !strings.HasPrefix(got, "#") || got[i+1:] != want {
		t.Errorf("got:\n%s", got)
	}
}

func TestWriteDVBv5(t *testing.T) {
	var buf bytes.Buffer
	if err := sample().WriteDVBv5(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "[BS01_0]\n\tDELIVERY_SYSTEM = ISDBS\n\tFREQUENCY = 1049480\n\tSTREAM_ID = 16400\n") {
		t.Errorf("got:\n%s", buf.String())
	}
}

func TestWriteCH2(t *testing.T) {
	var buf bytes.Buffer
	if err := sample().WriteCH2(&buf, CH2Options{Space: 1, SlotsPerTransponder: 4}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, line := range []string{
		"BS朝日1,1,0,0,1,151,4,16400,1\n",
		"BS朝日2,1,0,0,1,152,4,16400,1\n",
		"NHK BS,1,28,0,1,101,4,16625,1\n",
		"BS10: プレミアム,1,30,0,1,200,4,18675,1\n",
	} {
		if !strings.Contains(got, line) {
			t.Errorf("%q が無い:\n%s", line, got)
		}
	}
	if strings.Contains(got, "データ") {
		t.Error("データ放送まで書いた")
	}
	if err := sample().WriteCH2(&buf, CH2Options{Space: 1, SlotsPerTransponder: 2}); err == nil {
		t.Error("スロット 2 が 2 に収まってしまった")
	}
}

func TestDiff(t *testing.T) {
	a := sample()
	b := sample()
	if d := Diff(a, b); len(d) != 0 {
		t.Errorf("同じ表で %q", d)
	}
	b.BS[2].TSID = 0x4af3
	b.BS = append(b.BS[:1:1], b.BS[2], Entry{Channel: "BS15_1", Transponder: 15, Slot: 1, TSID: 0x40f2})
	d := Diff(a, b)
	want := []string{
		"- BS15_0 0x40f1 NHK BS（無くなった）",
		"+ BS15_1 0x40f2 BS15_1（増えた）",
		"~ BS15_2 TSID 0x48f3 → 0x4af3",
	}
	if strings.Join(d, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s", strings.Join(d, "\n"))
	}
}
