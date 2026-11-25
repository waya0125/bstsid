// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package scan

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/waya0125/bstsid/psi"
	"github.com/waya0125/bstsid/si"
	"github.com/waya0125/bstsid/table"
)

func section(tableID byte, ext uint16, ver, num, last byte, body []byte) []byte {
	n := 5 + len(body) + 4
	s := []byte{tableID, 0xb0 | byte(n>>8), byte(n), byte(ext >> 8), byte(ext), 0xc1 | ver<<1, num, last}
	s = append(s, body...)
	return binary.BigEndian.AppendUint32(s, psi.CRC32(s))
}

// packetize はセクションを続けて並べ、188 バイトのパケットに分ける。
func packetize(pid uint16, cc *byte, secs ...[]byte) []byte {
	var stream []byte
	for _, s := range secs {
		stream = append(stream, s...)
	}
	var out []byte
	first := true
	for len(stream) > 0 || first {
		p := make([]byte, 188)
		for i := range p {
			p[i] = 0xff
		}
		p[0] = 0x47
		p[1] = byte(pid>>8) & 0x1f
		p[2] = byte(pid)
		p[3] = 0x10 | *cc&0x0f
		*cc++
		off := 4
		if first {
			p[1] |= 0x40
			p[4] = 0
			off = 5
			first = false
		}
		n := copy(p[off:], stream)
		stream = stream[n:]
		out = append(out, p...)
	}
	return out
}

func satDelivery(freqKHz uint32) []byte {
	v := freqKHz / 10
	d := make([]byte, 11)
	for i := 7; i >= 0; i-- {
		digit := byte(v % 10)
		v /= 10
		if i%2 == 0 {
			d[i/2] |= digit << 4
		} else {
			d[i/2] |= digit
		}
	}
	d[4], d[5] = 0x11, 0x00 // 110.0 度
	d[6] = 0x80 | 0x20 | 0x08
	d[7], d[8], d[9], d[10] = 0x02, 0x88, 0x60, 0x03
	return append([]byte{0x43, 11}, d...)
}

type nitTS struct {
	tsid uint16
	freq uint32
	sids []uint16
}

func nitBody(name []byte, streams []nitTS) []byte {
	nd := append([]byte{0x40, byte(len(name))}, name...)
	b := []byte{0xf0 | byte(len(nd)>>8), byte(len(nd))}
	b = append(b, nd...)
	var loop []byte
	for _, s := range streams {
		ds := satDelivery(s.freq)
		sl := []byte{0x41, byte(3 * len(s.sids))}
		for _, sid := range s.sids {
			sl = append(sl, byte(sid>>8), byte(sid), 0x01)
		}
		ds = append(ds, sl...)
		loop = append(loop, byte(s.tsid>>8), byte(s.tsid), 0x00, 0x04, 0xf0|byte(len(ds)>>8), byte(len(ds)))
		loop = append(loop, ds...)
	}
	b = append(b, 0xf0|byte(len(loop)>>8), byte(len(loop)))
	return append(b, loop...)
}

func sdtBody(sid uint16, name string) []byte {
	sd := []byte{0x48, byte(3 + len(name)), 0x01, 0, byte(len(name))}
	sd = append(sd, name...)
	b := []byte{0x00, 0x04, 0xff}
	b = append(b, byte(sid>>8), byte(sid), 0xfc, 0x80|byte(len(sd)>>8), byte(len(sd)))
	return append(b, sd...)
}

// recisdb 1.3.2 同梱 dvbv5_channels_isdbs.conf の BS の TSID（recgvmvp の表と同じ、2026-11 時点）。
// Build のスロットの付け方がこの並びを再現できるか確かめる。
var recisdbBS = map[string]uint16{
	"BS01_0": 0x4010, "BS01_1": 0x4011, "BS01_2": 0x4012,
	"BS03_0": 0x4030, "BS03_1": 0x4631, "BS03_2": 0x4632,
	"BS05_0": 0x4450, "BS05_1": 0x4451,
	"BS07_0": 0x4470,
	"BS09_0": 0x4090, "BS09_1": 0x4092,
	"BS11_0": 0x46b0, "BS11_1": 0x46b1, "BS11_2": 0x46b2,
	"BS13_0": 0x40d0, "BS13_1": 0x40d1, "BS13_2": 0x46d2,
	"BS15_0": 0x40f1, "BS15_1": 0x40f2, "BS15_2": 0x48f3,
	"BS17_0": 0x4310,
	"BS19_0": 0x4730, "BS19_1": 0x4731, "BS19_2": 0x4732, "BS19_3": 0x4733,
	"BS21_0": 0x4750, "BS21_1": 0x4751, "BS21_2": 0x4752,
	"BS23_0": 0x4770, "BS23_1": 0x4971, "BS23_2": 0x4973,
}

func TestBuildMatchesRecisdb(t *testing.T) {
	// NIT の TS ループの並びに頼らないことを見るため、TSID の大きい順に渡す
	var streams []table.Stream
	for _, id := range []uint16{0x48f3, 0x4973, 0x4971, 0x4770, 0x4092, 0x4090, 0x46d2, 0x40d1, 0x40d0, 0x4632, 0x4631, 0x4030,
		0x40f2, 0x40f1, 0x4012, 0x4011, 0x4010, 0x4450, 0x4451, 0x4470, 0x46b2, 0x46b1, 0x46b0, 0x4310,
		0x4733, 0x4732, 0x4731, 0x4730, 0x4752, 0x4751, 0x4750} {
		tp := table.TransponderOfTSID(id)
		streams = append(streams, table.Stream{TSID: id, FreqKHz: table.FreqOfTransponder(tp)})
	}
	entries, warnings := table.Build(streams)
	for _, w := range warnings {
		t.Errorf("warning: %s", w)
	}
	if len(entries) != len(recisdbBS) {
		t.Fatalf("got %d entries, want %d", len(entries), len(recisdbBS))
	}
	for _, e := range entries {
		if want, ok := recisdbBS[e.Channel]; !ok || want != e.TSID {
			t.Errorf("%s = 0x%04x, recisdb は 0x%04x", e.Channel, e.TSID, want)
		}
	}
}

func TestCollectorEndToEnd(t *testing.T) {
	streams := []nitTS{
		{0x40f2, 11727480 + 7*38360, []uint16{102}},
		{0x40f1, 11727480 + 7*38360, []uint16{101}},
		{0x4010, 11727480, []uint16{151, 152}},
		{0x4011, 11727480, []uint16{161}},
	}
	// NIT を 2 セクションに分ける
	nit0 := section(si.TableNITActual, 4, 3, 0, 1, nitBody([]byte{0x0e, 'B', 'S'}, streams[:2]))
	nit1 := section(si.TableNITActual, 4, 3, 1, 1, nitBody([]byte{0x0e, 'B', 'S'}, streams[2:]))
	var ts []byte
	var ccNIT, ccSDT byte
	ts = append(ts, packetize(si.PIDNIT, &ccNIT, nit1)...) // 途中から受け始めた
	ts = append(ts, packetize(si.PIDSDT, &ccSDT,
		section(si.TableSDTActual, 0x40f1, 1, 0, 0, sdtBody(101, "\x0eNHK BS")),
		section(si.TableSDTOther, 0x40f2, 1, 0, 0, sdtBody(102, "\x0eNHK BS2")),
	)...)
	ts = append(ts, packetize(si.PIDNIT, &ccNIT, nit0)...)
	ts = append(ts, packetize(si.PIDSDT, &ccSDT,
		section(si.TableSDTOther, 0x4010, 1, 0, 0, sdtBody(151, "\x0eAsahi")),
	)...)

	c := NewCollector(true)
	if err := Run(bytes.NewReader(append([]byte{0x00, 0x47, 0x12}, ts...)), c, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if !c.NITComplete() {
		t.Fatal("NIT がそろっていない")
	}
	if c.Done() {
		t.Error("0x4011 の SDT が無いのに Done")
	}
	if m := c.MissingSDT(); len(m) != 1 || m[0] != 0x4011 {
		t.Errorf("MissingSDT = %x", m)
	}
	tb, warnings, err := c.Table(time.Date(2026, 11, 26, 7, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %q", warnings)
	}
	if tb.NetworkName != "BS" || tb.NITVersion != 3 || tb.NetworkID != 4 {
		t.Errorf("header = %+v", tb)
	}
	want := []struct {
		ch   string
		tsid uint16
		name string
	}{
		{"BS01_0", 0x4010, "Asahi"},
		{"BS01_1", 0x4011, "BS01_1"},
		{"BS15_0", 0x40f1, "NHK BS"},
		{"BS15_1", 0x40f2, "NHK BS2"},
	}
	if len(tb.BS) != len(want) {
		t.Fatalf("got %d entries: %+v", len(tb.BS), tb.BS)
	}
	for i, w := range want {
		e := tb.BS[i]
		if e.Channel != w.ch || e.TSID != w.tsid || e.Name() != w.name {
			t.Errorf("[%d] = %s 0x%04x %q, want %s 0x%04x %q", i, e.Channel, e.TSID, e.Name(), w.ch, w.tsid, w.name)
		}
	}
	// SDT が無い 0x4011 はサービスリスト記述子からサービス ID だけ入る
	if e, _ := tb.ByTSID(0x4011); len(e.Services) != 1 || e.Services[0].ServiceID != 161 {
		t.Errorf("0x4011 services = %+v", e.Services)
	}
}

func TestCollectorRejectsOtherNetwork(t *testing.T) {
	var cc byte
	ts := packetize(si.PIDNIT, &cc, section(si.TableNITActual, 6, 0, 0, 0, nitBody(nil, []nitTS{{0x6020, 12291000, nil}})))
	c := NewCollector(false)
	if err := Run(bytes.NewReader(ts), c, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Table(time.Now()); err == nil {
		t.Error("CS の NIT で表ができてしまった")
	}
}
