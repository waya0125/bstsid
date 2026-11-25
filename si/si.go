// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// Package si は ARIB STD-B10 の NIT と SDT のうち、BS の TSID 表を作るのに要る部分を読む。
package si

import (
	"errors"
	"fmt"
)

// テーブル ID。
const (
	TableNITActual = 0x40
	TableNITOther  = 0x41
	TableSDTActual = 0x42
	TableSDTOther  = 0x46
)

// PID。
const (
	PIDNIT = 0x0010
	PIDSDT = 0x0011
)

// 記述子タグ。
const (
	tagNetworkName       = 0x40
	tagServiceList       = 0x41
	tagSatelliteDelivery = 0x43
	tagService           = 0x48
)

// Header はセクションの共通部（section_syntax_indicator=1 のもの）。
type Header struct {
	TableID           byte
	TableIDExtension  uint16
	Version           byte
	CurrentNext       bool
	SectionNumber     byte
	LastSectionNumber byte
}

var errShort = errors.New("セクションが短い")

// ParseHeader はセクションの頭 8 バイトを読む。sec は CRC_32 まで含む 1 セクション。
func ParseHeader(sec []byte) (Header, error) {
	if len(sec) < 12 {
		return Header{}, errShort
	}
	if sec[1]&0x80 == 0 {
		return Header{}, fmt.Errorf("table_id 0x%02x: section_syntax_indicator が 0", sec[0])
	}
	return Header{
		TableID:           sec[0],
		TableIDExtension:  uint16(sec[3])<<8 | uint16(sec[4]),
		Version:           (sec[5] >> 1) & 0x1f,
		CurrentNext:       sec[5]&1 != 0,
		SectionNumber:     sec[6],
		LastSectionNumber: sec[7],
	}, nil
}

// Descriptor は 1 つの記述子。
type Descriptor struct {
	Tag  byte
	Data []byte
}

func parseDescriptors(b []byte) ([]Descriptor, error) {
	var out []Descriptor
	for len(b) > 0 {
		if len(b) < 2 || len(b) < 2+int(b[1]) {
			return out, errors.New("記述子が途中で切れている")
		}
		out = append(out, Descriptor{Tag: b[0], Data: b[2 : 2+int(b[1])]})
		b = b[2+int(b[1]):]
	}
	return out, nil
}

// SatelliteDelivery は衛星分配システム記述子（タグ 0x43）。
type SatelliteDelivery struct {
	// FrequencyKHz は周波数（kHz）。記述子では 10 kHz 単位の BCD 8 桁。
	FrequencyKHz uint32
	// OrbitalPosition は軌道位置（0.1 度単位、BCD 4 桁を数値にしたもの）。BS/110 度 CS は 1100。
	OrbitalPosition uint16
	East            bool
	Polarisation    byte
	Modulation      byte
	// SymbolRate はシンボルレート（100 sym/s 単位、BCD 7 桁を数値にしたもの）。
	SymbolRate uint32
	FECInner   byte
}

func bcd(b []byte, digits int) (uint32, bool) {
	var v uint32
	for i := range digits {
		d := b[i/2]
		if i%2 == 0 {
			d >>= 4
		}
		d &= 0x0f
		if d > 9 {
			return 0, false
		}
		v = v*10 + uint32(d)
	}
	return v, true
}

// ParseSatelliteDelivery は衛星分配システム記述子の中身を読む。
func ParseSatelliteDelivery(d []byte) (SatelliteDelivery, error) {
	if len(d) < 11 {
		return SatelliteDelivery{}, errors.New("衛星分配システム記述子が短い")
	}
	f, ok1 := bcd(d[0:4], 8)
	orb, ok2 := bcd(d[4:6], 4)
	sr, ok3 := bcd(d[7:11], 7)
	if !ok1 || !ok2 || !ok3 {
		return SatelliteDelivery{}, errors.New("衛星分配システム記述子の BCD が壊れている")
	}
	return SatelliteDelivery{
		FrequencyKHz:    f * 10,
		OrbitalPosition: uint16(orb),
		East:            d[6]&0x80 != 0,
		Polarisation:    (d[6] >> 5) & 0x3,
		Modulation:      d[6] & 0x1f,
		SymbolRate:      sr,
		FECInner:        d[10] & 0x0f,
	}, nil
}

// ServiceRef はサービスリスト記述子の 1 項目。
type ServiceRef struct {
	ServiceID   uint16
	ServiceType byte
}

// NITTransportStream は NIT の TS ループの 1 項目。
type NITTransportStream struct {
	TransportStreamID uint16
	OriginalNetworkID uint16
	// Delivery は衛星分配システム記述子。無ければ nil。
	Delivery *SatelliteDelivery
	// Services はサービスリスト記述子の中身。
	Services []ServiceRef
}

// NITSection は NIT の 1 セクション。
type NITSection struct {
	Header
	NetworkID uint16
	// NetworkName はネットワーク名記述子の文字列（ARIB 8 単位符号を復号したもの）。無ければ空。
	NetworkName string
	Streams     []NITTransportStream
}

// ParseNIT は NIT の 1 セクションを読む。
func ParseNIT(sec []byte) (NITSection, error) {
	h, err := ParseHeader(sec)
	if err != nil {
		return NITSection{}, err
	}
	if h.TableID != TableNITActual && h.TableID != TableNITOther {
		return NITSection{}, fmt.Errorf("NIT ではない（table_id 0x%02x）", h.TableID)
	}
	n := NITSection{Header: h, NetworkID: h.TableIDExtension}
	body := sec[8 : len(sec)-4]
	if len(body) < 2 {
		return n, errShort
	}
	ndl := int(body[0]&0x0f)<<8 | int(body[1])
	body = body[2:]
	if len(body) < ndl {
		return n, errShort
	}
	nds, err := parseDescriptors(body[:ndl])
	if err != nil {
		return n, err
	}
	for _, d := range nds {
		if d.Tag == tagNetworkName {
			n.NetworkName = DecodeString(d.Data)
		}
	}
	body = body[ndl:]
	if len(body) < 2 {
		return n, errShort
	}
	tll := int(body[0]&0x0f)<<8 | int(body[1])
	body = body[2:]
	if len(body) < tll {
		return n, errShort
	}
	body = body[:tll]
	for len(body) > 0 {
		if len(body) < 6 {
			return n, errShort
		}
		ts := NITTransportStream{
			TransportStreamID: uint16(body[0])<<8 | uint16(body[1]),
			OriginalNetworkID: uint16(body[2])<<8 | uint16(body[3]),
		}
		dl := int(body[4]&0x0f)<<8 | int(body[5])
		body = body[6:]
		if len(body) < dl {
			return n, errShort
		}
		ds, err := parseDescriptors(body[:dl])
		if err != nil {
			return n, err
		}
		body = body[dl:]
		for _, d := range ds {
			switch d.Tag {
			case tagSatelliteDelivery:
				sd, err := ParseSatelliteDelivery(d.Data)
				if err != nil {
					return n, fmt.Errorf("TSID 0x%04x: %w", ts.TransportStreamID, err)
				}
				ts.Delivery = &sd
			case tagServiceList:
				for b := d.Data; len(b) >= 3; b = b[3:] {
					ts.Services = append(ts.Services, ServiceRef{ServiceID: uint16(b[0])<<8 | uint16(b[1]), ServiceType: b[2]})
				}
			}
		}
		n.Streams = append(n.Streams, ts)
	}
	return n, nil
}

// SDTService は SDT のサービスループの 1 項目。
type SDTService struct {
	ServiceID   uint16
	ServiceType byte
	Provider    string
	Name        string
}

// SDTSection は SDT の 1 セクション。
type SDTSection struct {
	Header
	TransportStreamID uint16
	OriginalNetworkID uint16
	Services          []SDTService
}

// ParseSDT は SDT の 1 セクションを読む。
func ParseSDT(sec []byte) (SDTSection, error) {
	h, err := ParseHeader(sec)
	if err != nil {
		return SDTSection{}, err
	}
	if h.TableID != TableSDTActual && h.TableID != TableSDTOther {
		return SDTSection{}, fmt.Errorf("SDT ではない（table_id 0x%02x）", h.TableID)
	}
	s := SDTSection{Header: h, TransportStreamID: h.TableIDExtension}
	body := sec[8 : len(sec)-4]
	if len(body) < 3 {
		return s, errShort
	}
	s.OriginalNetworkID = uint16(body[0])<<8 | uint16(body[1])
	body = body[3:]
	for len(body) > 0 {
		if len(body) < 5 {
			return s, errShort
		}
		svc := SDTService{ServiceID: uint16(body[0])<<8 | uint16(body[1])}
		dl := int(body[3]&0x0f)<<8 | int(body[4])
		body = body[5:]
		if len(body) < dl {
			return s, errShort
		}
		ds, err := parseDescriptors(body[:dl])
		if err != nil {
			return s, err
		}
		body = body[dl:]
		for _, d := range ds {
			if d.Tag != tagService || len(d.Data) < 2 {
				continue
			}
			b := d.Data
			svc.ServiceType = b[0]
			pl := int(b[1])
			if len(b) < 2+pl+1 {
				continue
			}
			svc.Provider = DecodeString(b[2 : 2+pl])
			b = b[2+pl:]
			nl := int(b[0])
			if len(b) < 1+nl {
				continue
			}
			svc.Name = DecodeString(b[1 : 1+nl])
		}
		s.Services = append(s.Services, svc)
	}
	return s, nil
}
