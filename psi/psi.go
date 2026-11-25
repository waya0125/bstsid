// SPDX-License-Identifier: MIT
// Copyright (c) 2026 KASHIWAYA Rikuto

// Package psi は MPEG-2 TS のパケットを読み、指定した PID の PSI/SI セクションを組み立てる。
package psi

import (
	"bufio"
	"errors"
	"io"
)

// PacketSize は TS パケットの大きさ。
const PacketSize = 188

const syncByte = 0x47

// Reader は io.Reader から TS パケットを 1 つずつ取り出す。
// 先頭や途中で同期がずれていても、0x47 が 188 バイトおきに並ぶ位置を探して合わせ直す。
type Reader struct {
	r   *bufio.Reader
	pkt [PacketSize]byte
}

// NewReader は r から読む Reader を作る。
func NewReader(r io.Reader) *Reader {
	return &Reader{r: bufio.NewReaderSize(r, PacketSize*512)}
}

// Next は次のパケットを返す。返した値は次の呼び出しまで有効。
// 入力が尽きたら io.EOF を返す（最後の半端なパケットは捨てる）。
func (r *Reader) Next() ([]byte, error) {
	for {
		b, err := r.r.Peek(PacketSize + 1)
		if len(b) < PacketSize {
			if err == nil || errors.Is(err, io.EOF) || errors.Is(err, bufio.ErrBufferFull) {
				return nil, io.EOF
			}
			return nil, err
		}
		// 次のパケットの頭まで見えていればそれも確かめる。末尾の 1 パケットは頭だけで判断する。
		if b[0] == syncByte && (len(b) == PacketSize || b[PacketSize] == syncByte) {
			copy(r.pkt[:], b[:PacketSize])
			if _, err := r.r.Discard(PacketSize); err != nil {
				return nil, err
			}
			return r.pkt[:], nil
		}
		if _, err := r.r.Discard(1); err != nil {
			return nil, err
		}
	}
}

// PID はパケットの PID を返す。
func PID(pkt []byte) uint16 { return uint16(pkt[1]&0x1f)<<8 | uint16(pkt[2]) }

// payload はパケットのペイロードと payload_unit_start_indicator を返す。
// エラー表示のあるパケットやペイロードの無いパケットでは ok が false。
func payload(pkt []byte) (p []byte, pusi bool, cc byte, ok bool) {
	if len(pkt) != PacketSize || pkt[0] != syncByte || pkt[1]&0x80 != 0 {
		return nil, false, 0, false
	}
	pusi = pkt[1]&0x40 != 0
	afc := (pkt[3] >> 4) & 0x3
	cc = pkt[3] & 0x0f
	off := 4
	switch afc {
	case 1:
	case 3:
		off += 1 + int(pkt[4])
	default:
		return nil, false, 0, false
	}
	if off >= PacketSize {
		return nil, false, 0, false
	}
	return pkt[off:], pusi, cc, true
}

// Assembler は 1 つの PID のパケットからセクションを組み立てる。
type Assembler struct {
	buf     []byte
	started bool
	lastCC  int
}

// NewAssembler は空の Assembler を作る。
func NewAssembler() *Assembler { return &Assembler{lastCC: -1} }

// Push はパケットを 1 つ渡し、そこで出来上がったセクションを返す。
// CRC_32 が合わないセクションは捨てる。返したスライスは呼び出し側のもの。
func (a *Assembler) Push(pkt []byte) [][]byte {
	p, pusi, cc, ok := payload(pkt)
	if !ok {
		return nil
	}
	if a.lastCC >= 0 && byte(a.lastCC) == cc {
		return nil // 重送パケット
	}
	if a.lastCC >= 0 && byte((a.lastCC+1)&0x0f) != cc {
		// 抜けがあった。組み立て中のものは捨てる
		a.started = false
		a.buf = a.buf[:0]
	}
	a.lastCC = int(cc)

	var out [][]byte
	if pusi {
		ptr := int(p[0])
		p = p[1:]
		if ptr > len(p) {
			a.started = false
			a.buf = a.buf[:0]
			return nil
		}
		if a.started {
			a.buf = append(a.buf, p[:ptr]...)
			out = a.drain(out)
		}
		a.buf = append(a.buf[:0], p[ptr:]...)
		a.started = true
		return a.drain(out)
	}
	if !a.started {
		return nil
	}
	a.buf = append(a.buf, p...)
	return a.drain(out)
}

// drain は buf の頭から出来上がったセクションを取り出す。
func (a *Assembler) drain(out [][]byte) [][]byte {
	for {
		if len(a.buf) == 0 || a.buf[0] == 0xff {
			// 詰め物。次の PUSI まで何も無い
			a.buf = a.buf[:0]
			a.started = false
			return out
		}
		if len(a.buf) < 3 {
			return out
		}
		n := 3 + (int(a.buf[1]&0x0f)<<8 | int(a.buf[2]))
		if len(a.buf) < n {
			return out
		}
		sec := append([]byte(nil), a.buf[:n]...)
		a.buf = a.buf[n:]
		if sec[1]&0x80 == 0 || CRC32(sec) == 0 {
			out = append(out, sec)
		}
	}
}

var crcTable = func() (t [256]uint32) {
	for i := range t {
		c := uint32(i) << 24
		for range 8 {
			if c&0x80000000 != 0 {
				c = c<<1 ^ 0x04c11db7
			} else {
				c <<= 1
			}
		}
		t[i] = c
	}
	return
}()

// CRC32 は MPEG-2 の CRC_32（多項式 0x04c11db7、初期値 0xffffffff、反転なし）を返す。
// CRC_32 フィールドまで含めたセクション全体に掛けると 0 になる。
func CRC32(b []byte) uint32 {
	c := uint32(0xffffffff)
	for _, x := range b {
		c = c<<8 ^ crcTable[byte(c>>24)^x]
	}
	return c
}
