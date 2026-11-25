// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// Package scan は BS の TS から NIT と SDT を集めて TSID 表を作る。
package scan

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/waya0125/bstsid/psi"
	"github.com/waya0125/bstsid/si"
	"github.com/waya0125/bstsid/table"
)

// NetworkIDBS は BS デジタルの original_network_id / network_id。
const NetworkIDBS = 0x0004

// sections は 1 つのサブテーブル（table_id と table_id_extension の組）のセクションの集まり。
type sections struct {
	version byte
	last    byte
	got     map[byte][]byte
}

func (s *sections) complete() bool { return s != nil && len(s.got) == int(s.last)+1 }

func (s *sections) ordered() [][]byte {
	out := make([][]byte, 0, len(s.got))
	for i := 0; i <= int(s.last); i++ {
		out = append(out, s.got[byte(i)])
	}
	return out
}

type key struct {
	table byte
	ext   uint16
}

// Collector は TS のパケットを受け取り、NIT（自ネットワーク）と SDT を集める。
type Collector struct {
	// WantSDT が false なら NIT がそろった時点で Done になる（局名は入らない）。
	WantSDT bool

	nitAsm, sdtAsm *psi.Assembler
	subs           map[key]*sections
	nit            *sections // そろった NIT（自ネットワーク）
	nitID          uint16
	foreignNIT     uint16 // BS 以外の NIT を見たときの network_id
}

// NewCollector は Collector を作る。
func NewCollector(wantSDT bool) *Collector {
	return &Collector{
		WantSDT: wantSDT,
		nitAsm:  psi.NewAssembler(),
		sdtAsm:  psi.NewAssembler(),
		subs:    map[key]*sections{},
	}
}

// Push はパケットを 1 つ渡す。
func (c *Collector) Push(pkt []byte) {
	var secs [][]byte
	switch psi.PID(pkt) {
	case si.PIDNIT:
		secs = c.nitAsm.Push(pkt)
	case si.PIDSDT:
		secs = c.sdtAsm.Push(pkt)
	default:
		return
	}
	for _, sec := range secs {
		c.addSection(sec)
	}
}

func (c *Collector) addSection(sec []byte) {
	h, err := si.ParseHeader(sec)
	if err != nil || !h.CurrentNext {
		return
	}
	switch h.TableID {
	case si.TableNITActual:
		if h.TableIDExtension != NetworkIDBS {
			c.foreignNIT = h.TableIDExtension
			return
		}
	case si.TableSDTActual, si.TableSDTOther:
	default:
		return
	}
	k := key{h.TableID, h.TableIDExtension}
	s := c.subs[k]
	if s == nil || s.version != h.Version || s.last != h.LastSectionNumber {
		// 初めて見たか、版が変わった。集め直す
		s = &sections{version: h.Version, last: h.LastSectionNumber, got: map[byte][]byte{}}
		c.subs[k] = s
	}
	if h.SectionNumber > s.last {
		return
	}
	s.got[h.SectionNumber] = sec
	if h.TableID == si.TableNITActual && s.complete() {
		c.nit = s
		c.nitID = h.TableIDExtension
	}
}

// NITComplete は NIT がそろったか。
func (c *Collector) NITComplete() bool { return c.nit != nil }

// Done は表を作るのに要るものがそろったか（NIT と、WantSDT なら NIT に載っている全 TS の SDT）。
func (c *Collector) Done() bool {
	if c.nit == nil {
		return false
	}
	if !c.WantSDT {
		return true
	}
	return len(c.missingSDT()) == 0
}

// nitStreams はそろった NIT の BS の TS ループを返す。
func (c *Collector) nitStreams() (name string, version byte, streams []si.NITTransportStream, err error) {
	for _, sec := range c.nit.ordered() {
		n, err := si.ParseNIT(sec)
		if err != nil {
			return "", 0, nil, fmt.Errorf("NIT section %d: %w", n.SectionNumber, err)
		}
		if n.NetworkName != "" {
			name = n.NetworkName
		}
		version = n.Version
		for _, ts := range n.Streams {
			if ts.OriginalNetworkID == NetworkIDBS {
				streams = append(streams, ts)
			}
		}
	}
	return name, version, streams, nil
}

func (c *Collector) sdtFor(tsid uint16) *sections {
	if s := c.subs[key{si.TableSDTActual, tsid}]; s.complete() {
		return s
	}
	if s := c.subs[key{si.TableSDTOther, tsid}]; s.complete() {
		return s
	}
	return nil
}

func (c *Collector) missingSDT() []uint16 {
	_, _, streams, err := c.nitStreams()
	if err != nil {
		return nil
	}
	var out []uint16
	for _, ts := range streams {
		if c.sdtFor(ts.TransportStreamID) == nil {
			out = append(out, ts.TransportStreamID)
		}
	}
	return out
}

// MissingSDT は NIT に載っているのに SDT がまだそろっていない TSID を返す。
func (c *Collector) MissingSDT() []uint16 { return c.missingSDT() }

// ErrNoNIT は NIT がそろわなかったときのエラー。
var ErrNoNIT = errors.New("NIT（PID 0x0010）がそろわなかった。BS を選局した TS を 15 秒以上渡しているか確かめてください")

// Table はここまでに集めたもので表を作る。SDT が足りない TS はサービスを NIT のサービスリスト記述子から取る（名前なし）。
func (c *Collector) Table(now time.Time) (*table.Table, []string, error) {
	if c.nit == nil {
		if c.foreignNIT != 0 {
			return nil, nil, fmt.Errorf("BS ではない NIT（network_id 0x%04x）しか無かった。BS のチャンネルを選局した TS を渡してください", c.foreignNIT)
		}
		return nil, nil, ErrNoNIT
	}
	name, version, nstreams, err := c.nitStreams()
	if err != nil {
		return nil, nil, err
	}
	var warnings []string
	var streams []table.Stream
	for _, ts := range nstreams {
		st := table.Stream{TSID: ts.TransportStreamID}
		if ts.Delivery != nil {
			st.FreqKHz = ts.Delivery.FrequencyKHz
		}
		if sdt := c.sdtFor(ts.TransportStreamID); sdt != nil {
			for _, sec := range sdt.ordered() {
				s, err := si.ParseSDT(sec)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("TSID 0x%04x の SDT: %v", ts.TransportStreamID, err))
					continue
				}
				for _, svc := range s.Services {
					st.Services = append(st.Services, table.Service{ServiceID: svc.ServiceID, Type: svc.ServiceType, Name: svc.Name})
				}
			}
		} else {
			if c.WantSDT {
				warnings = append(warnings, fmt.Sprintf("TSID 0x%04x: SDT がそろわなかったので局名なし", ts.TransportStreamID))
			}
			for _, r := range ts.Services {
				st.Services = append(st.Services, table.Service{ServiceID: r.ServiceID, Type: r.ServiceType})
			}
		}
		sort.Slice(st.Services, func(i, j int) bool { return st.Services[i].ServiceID < st.Services[j].ServiceID })
		streams = append(streams, st)
	}
	entries, w := table.Build(streams)
	warnings = append(warnings, w...)
	if len(entries) == 0 {
		return nil, warnings, errors.New("NIT に BS の TS が 1 つも無かった")
	}
	return &table.Table{
		Format:      table.FormatVersion,
		GeneratedAt: now,
		NetworkID:   c.nitID,
		NetworkName: name,
		NITVersion:  int(version),
		BS:          entries,
	}, warnings, nil
}

// Run は r から TS を読み、Done になるか入力が尽きるか deadline を過ぎるまで集める。
// deadline がゼロ値なら時間では止めない。
func Run(r io.Reader, c *Collector, deadline time.Time) error {
	pr := psi.NewReader(r)
	for n := 0; ; n++ {
		pkt, err := pr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		c.Push(pkt)
		if n%1024 == 0 {
			if c.Done() {
				return nil
			}
			if !deadline.IsZero() && time.Now().After(deadline) {
				return nil
			}
		}
	}
}
