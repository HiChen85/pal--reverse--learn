// Package palcodec decodes resource compression formats without executing them.
package palcodec

import (
	"encoding/binary"
	"fmt"
)

// Format research reference: SDLPAL yj1.c, Wei Mingzhi and SDLPAL contributors,
// portions attributed there to Lou Yihua's PalLibrary (GPL-3.0-or-later).
// https://github.com/sdlpal/sdlpal/blob/master/yj1.c
// This Go decoder independently expresses the documented file/tree/block layout
// with checked indexes, an explicit bit reader, and exact output accounting.

const MaxYJ1Bytes = 64 << 20

type yjNode struct {
	value       byte
	internal    bool
	left, right int
}

// yjBits reads high bits first from each little-endian 16-bit storage word.
// Storage words are not a conventional byte-wise MSB or LSB bit stream.
type yjBits struct {
	data []byte
	bit  int
}

func (r *yjBits) read(n int) (uint16, error) {
	if n < 0 || n > 16 {
		return 0, fmt.Errorf("YJ1: invalid bit-code width %d", n)
	}
	var v uint16
	for i := 0; i < n; i++ {
		base := (r.bit / 16) * 2
		if base+1 >= len(r.data) {
			return 0, fmt.Errorf("YJ1: truncated bit stream at bit %d", r.bit)
		}
		w := binary.LittleEndian.Uint16(r.data[base : base+2])
		v = v<<1 | ((w >> uint(15-r.bit%16)) & 1)
		r.bit++
	}
	return v, nil
}

type yjBlock struct {
	repeat                    [4]uint16
	offsetWidths              [4]byte
	repeatWidths, countWidths [3]byte
	counts                    [2]byte
}

func (b yjBlock) loop(r *yjBits) (int, error) {
	v, err := r.read(1)
	if err != nil {
		return 0, err
	}
	if v == 1 {
		return int(b.counts[0]), nil
	}
	v, err = r.read(2)
	if err != nil {
		return 0, err
	}
	if v == 0 {
		return int(b.counts[1]), nil
	}
	n, err := r.read(int(b.countWidths[v-1]))
	return int(n), err
}
func (b yjBlock) repetitions(r *yjBits) (int, error) {
	index, err := r.read(2)
	if err != nil {
		return 0, err
	}
	if index == 0 {
		return int(b.repeat[0]), nil
	}
	variable, err := r.read(1)
	if err != nil {
		return 0, err
	}
	if variable == 0 {
		return int(b.repeat[index]), nil
	}
	n, err := r.read(int(b.repeatWidths[index-1]))
	return int(n), err
}

// DecodeYJ1 returns a new buffer exactly as long as the YJ_1 declared output.
// maxOutput must be between zero and MaxYJ1Bytes (64 MiB). The declared compressed
// span is also limited to 64 MiB. Counts, trees, blocks, bit codes and backwards
// references are checked before use; a malformed stream returns no partial data.
// Unused bits/bytes inside a compressed block and bytes after the last block,
// including bytes outside the declared compressed span, are accepted as padding.
// Padding is never interpreted as another block or returned in the output.
func DecodeYJ1(data []byte, maxOutput int) ([]byte, error) {
	if maxOutput < 0 || maxOutput > MaxYJ1Bytes {
		return nil, fmt.Errorf("YJ1: maxOutput outside 0..%d", MaxYJ1Bytes)
	}
	if len(data) < 16 || string(data[:4]) != "YJ_1" {
		return nil, fmt.Errorf("YJ1: missing 16-byte YJ_1 header")
	}
	outputLen := uint64(binary.LittleEndian.Uint32(data[4:8]))
	if outputLen > uint64(maxOutput) {
		return nil, fmt.Errorf("YJ1: output length %d exceeds limit %d", outputLen, maxOutput)
	}
	span := uint64(binary.LittleEndian.Uint32(data[8:12]))
	if span < 16 || span > uint64(len(data)) || span > MaxYJ1Bytes {
		return nil, fmt.Errorf("YJ1: invalid compressed length %d", span)
	}
	data = data[:int(span)]
	blocks := int(binary.LittleEndian.Uint16(data[12:14]))
	nodes := int(data[15]) * 2
	flagBytes := ((nodes + 15) / 16) * 2
	cursor := 16 + nodes + flagBytes
	if cursor > len(data) {
		return nil, fmt.Errorf("YJ1: truncated tree")
	}
	tree := make([]yjNode, nodes+1)
	tree[0] = yjNode{internal: true, left: 1, right: 2}
	flags := yjBits{data: data[16+nodes : cursor]}
	for i := 1; i <= nodes; i++ {
		internal, err := flags.read(1)
		if err != nil {
			return nil, err
		}
		tree[i] = yjNode{value: data[15+i], internal: internal == 1}
		if tree[i].internal {
			tree[i].left = int(tree[i].value)*2 + 1
			tree[i].right = tree[i].left + 1
			if tree[i].right > nodes {
				return nil, fmt.Errorf("YJ1: tree node %d points outside tree", i)
			}
		}
	}
	if nodes > 0 {
		color := make([]byte, len(tree))
		var visit func(int) error
		visit = func(i int) error {
			if color[i] == 1 {
				return fmt.Errorf("YJ1: cyclic Huffman tree")
			}
			if color[i] == 2 {
				return nil
			}
			color[i] = 1
			if tree[i].internal {
				if err := visit(tree[i].left); err != nil {
					return err
				}
				if err := visit(tree[i].right); err != nil {
					return err
				}
			}
			color[i] = 2
			return nil
		}
		for i := range tree {
			if err := visit(i); err != nil {
				return nil, err
			}
		}
	}
	out := make([]byte, 0, int(outputLen))
	for index := 0; index < blocks; index++ {
		if cursor > len(data)-4 {
			return nil, fmt.Errorf("YJ1: truncated block %d header", index)
		}
		plain := int(binary.LittleEndian.Uint16(data[cursor : cursor+2]))
		packed := int(binary.LittleEndian.Uint16(data[cursor+2 : cursor+4]))
		if plain > 0x4000 || plain > int(outputLen)-len(out) {
			return nil, fmt.Errorf("YJ1: block %d output length out of bounds", index)
		}
		endOutput := len(out) + plain
		if packed == 0 {
			if plain > len(data)-cursor-4 {
				return nil, fmt.Errorf("YJ1: truncated raw block %d", index)
			}
			out = append(out, data[cursor+4:cursor+4+plain]...)
			cursor += 4 + plain
			continue
		}
		if packed < 24 || packed > len(data)-cursor {
			return nil, fmt.Errorf("YJ1: invalid compressed block %d length %d", index, packed)
		}
		if nodes == 0 {
			return nil, fmt.Errorf("YJ1: compressed block has no Huffman tree")
		}
		h := data[cursor : cursor+24]
		b := yjBlock{}
		for i := range b.repeat {
			b.repeat[i] = binary.LittleEndian.Uint16(h[4+i*2 : 6+i*2])
		}
		copy(b.offsetWidths[:], h[12:16])
		copy(b.repeatWidths[:], h[16:19])
		copy(b.countWidths[:], h[19:22])
		copy(b.counts[:], h[22:24])
		for _, widths := range [][]byte{b.offsetWidths[:], b.repeatWidths[:], b.countWidths[:]} {
			for _, w := range widths {
				if w > 16 {
					return nil, fmt.Errorf("YJ1: block %d invalid code width %d", index, w)
				}
			}
		}
		r := yjBits{data: data[cursor+24 : cursor+packed]}
		for {
			literals, err := b.loop(&r)
			if err != nil {
				return nil, fmt.Errorf("YJ1 block %d: %w", index, err)
			}
			if literals == 0 {
				break
			}
			if literals > endOutput-len(out) {
				return nil, fmt.Errorf("YJ1: block %d literal run exceeds output", index)
			}
			for i := 0; i < literals; i++ {
				n := 0
				for tree[n].internal {
					bit, err := r.read(1)
					if err != nil {
						return nil, err
					}
					if bit == 0 {
						n = tree[n].left
					} else {
						n = tree[n].right
					}
				}
				out = append(out, tree[n].value)
			}
			runs, err := b.loop(&r)
			if err != nil {
				return nil, err
			}
			if runs == 0 {
				break
			}
			for i := 0; i < runs; i++ {
				count, err := b.repetitions(&r)
				if err != nil {
					return nil, err
				}
				which, err := r.read(2)
				if err != nil {
					return nil, err
				}
				distance, err := r.read(int(b.offsetWidths[which]))
				if err != nil {
					return nil, err
				}
				if distance == 0 || int(distance) > len(out) {
					return nil, fmt.Errorf("YJ1: invalid backward distance %d", distance)
				}
				if count > endOutput-len(out) {
					return nil, fmt.Errorf("YJ1: block %d repeat exceeds output", index)
				}
				for j := 0; j < count; j++ {
					out = append(out, out[len(out)-int(distance)])
				}
			}
		}
		if len(out) != endOutput {
			return nil, fmt.Errorf("YJ1: block %d produced %d bytes, want %d", index, len(out)-(endOutput-plain), plain)
		}
		cursor += packed
	}
	if len(out) != int(outputLen) {
		return nil, fmt.Errorf("YJ1: output length %d differs from declared %d", len(out), outputLen)
	}
	return out, nil
}
