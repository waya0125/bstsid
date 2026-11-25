// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

package psi

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func sec(id byte, payloadLen int) []byte {
	n := 5 + payloadLen + 4
	s := []byte{id, 0xb0 | byte(n>>8), byte(n), 0, 1, 0xc1, 0, 0}
	for i := range payloadLen {
		s = append(s, byte(i))
	}
	return binary.BigEndian.AppendUint32(s, CRC32(s))
}

func pkt(pusi bool, cc byte, data []byte) []byte {
	p := bytes.Repeat([]byte{0xff}, PacketSize)
	p[0], p[1], p[2], p[3] = 0x47, 0x00, 0x10, 0x10|cc
	if pusi {
		p[1] |= 0x40
	}
	copy(p[4:], data)
	return p
}

func TestCRC32(t *testing.T) {
	s := sec(0x40, 10)
	if CRC32(s) != 0 {
		t.Error("CRC が 0 にならない")
	}
}

func TestAssemblerTwoSectionsAndSplit(t *testing.T) {
	a, b, c := sec(0x40, 20), sec(0x42, 30), sec(0x46, 200)
	// 1 パケットに a・b と c の頭、次のパケットに c の残り
	first := append([]byte{0}, a...)
	first = append(first, b...)
	room := PacketSize - 4 - len(first)
	first = append(first, c[:room]...)
	as := NewAssembler()
	got := as.Push(pkt(true, 0, first))
	if len(got) != 2 || !bytes.Equal(got[0], a) || !bytes.Equal(got[1], b) {
		t.Fatalf("1 つ目のパケットで %d セクション", len(got))
	}
	// 重送は無視される
	if got := as.Push(pkt(true, 0, first)); got != nil {
		t.Error("重送パケットを読んだ")
	}
	got = as.Push(pkt(false, 1, c[room:]))
	if len(got) != 1 || !bytes.Equal(got[0], c) {
		t.Fatalf("2 つ目のパケットで %d セクション", len(got))
	}
}

func TestAssemblerDropsOnDiscontinuityAndBadCRC(t *testing.T) {
	c := sec(0x46, 300)
	as := NewAssembler()
	as.Push(pkt(true, 0, append([]byte{0}, c[:150]...)))
	if got := as.Push(pkt(false, 2, c[150:])); got != nil {
		t.Error("抜けのあとでセクションを返した")
	}
	bad := sec(0x40, 10)
	bad[10] ^= 1
	if got := NewAssembler().Push(pkt(true, 0, append([]byte{0}, bad...))); got != nil {
		t.Error("CRC が合わないセクションを返した")
	}
}

func TestReaderResync(t *testing.T) {
	p := pkt(true, 0, nil)
	in := append([]byte{1, 2, 0x47, 3}, p...)
	in = append(in, p...)
	in = append(in, p[:100]...) // 最後は半端
	r := NewReader(bytes.NewReader(in))
	n := 0
	for {
		_, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 2 {
		t.Errorf("%d パケット", n)
	}
}
