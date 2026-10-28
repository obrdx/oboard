// SPDX-License-Identifier: GPL-3.0-or-later
// Derived from github.com/sagernet/sing-snell 20f2eaec77c3 snellv6/profile.go
// and snellv6/salt.go. Only the derivation that fixes the first record of a
// default-mode connection is kept. testdata/snell_v6_shape_vectors.json pins it
// to the kernel copy, which verifies the same vectors.

package core

import (
	"encoding/binary"
	"math/bits"

	"golang.org/x/crypto/blake2b"
)

const (
	snellV6GoldenGamma     = 0x9e3779b97f4a7c15
	snellV6SaltLen         = 16
	snellV6HeaderCipherLen = 7 + 16
	snellV6AEADTagLen      = 16
	snellV6MaxPayload      = 0xffff
	snellV6MaxExtraPadding = 0x02da
	snellV6SaltNSXor       = 0xdaa66d2c7ddf743f
)

const (
	snellV6LabelPadding       = 0
	snellV6LabelBitPercent    = 1
	snellV6LabelMotif         = 2
	snellV6LabelMixOffset     = 3
	snellV6LabelSalt          = 3
	snellV6LabelProfileID     = 5
	snellV6LabelGenerator     = 6
	snellV6LabelPadMin        = 7
	snellV6LabelPadMax        = 8
	snellV6LabelPadCount      = 9
	snellV6LabelPadInterval   = 10
	snellV6LabelSmallLimit    = 11
	snellV6LabelBitMin        = 12
	snellV6LabelBitMax        = 13
	snellV6LabelPrefixMin     = 14
	snellV6LabelPrefixMax     = 15
	snellV6LabelMixMode       = 16
	snellV6LabelMixRounds     = 17
	snellV6LabelMixStride     = 18
	snellV6LabelMixOffsetBase = 19
	snellV6LabelMixBlock      = 20
	snellV6LabelChunkPolicy   = 21
	snellV6LabelChunkInitial  = 22
	snellV6LabelChunkFirstCap = 22
	snellV6LabelChunkMax      = 23
	snellV6LabelChunkStep     = 24
	snellV6LabelChunkJitter   = 25
	snellV6LabelChunkBucket   = 26
	snellV6LabelWritePolicy   = 28
	snellV6LabelWriteFirst    = 29
	snellV6LabelWriteBucket   = 30
	snellV6LabelWriteSeq      = 31
	snellV6LabelWriteJitter   = 32
	snellV6LabelRecordPrefix  = 33
	snellV6LabelPayloadPad    = 34
	snellV6LabelWriteTarget   = 35
	snellV6LabelWriteJitterV  = 36
	snellV6LabelWriteNext     = 37
	snellV6LabelChunkSize     = 38
	snellV6LabelChunkJitterV  = 39
)

const (
	snellV6HandshakeDomain    = 0x7053
	snellV6MixHandshakeDomain = 0x51a7
	snellV6ChunkInitialDomain = 0xf17c
)

const (
	snellV6NSSeedProfile = 0xb46c2e7d9a1538f1
	snellV6NSSeedPrefix  = 0x5d9217c083e64ab9
	snellV6NSSeedMotif   = 0xa71f0c54d8396e2b
	snellV6NSSeedSalt    = 0x3e8a91b52740f6cd
	snellV6NSSeedMix     = 0xc9f4260b7d1e835a
	snellV6NSSeedChunk   = 0x62d0b5e19c4a783f
	snellV6NSSeedWrite   = 0x917b3c48e6a205d4
)

const (
	snellV6DomainMul    = 0xd6e8feb86659fd93
	snellV6NamespaceAdd = 0xa0761d6478bd642f
	snellV6CoefB        = 0x589965cc75374cc3
	snellV6AddB         = 0x33a213ec50ffe2e9
	snellV6CoefA        = 0xe7037ed1a0b428db
	snellV6AddA         = 0x8f3907f7b2b80c35
)

var snellV6ProfileSeed = [...]byte{
	0x8d, 0x41, 0xa7, 0x13, 0x5c, 0xe2, 0x09, 0xbb, 0x70, 0x2f, 0xd6, 0x94,
	0x33, 0x18, 0xc0, 0x6e, 0x4a, 0x91, 0x25, 0xfd, 0xb8, 0x03, 0x77, 0xac,
}

var snellV6BitRotateTable = [...]byte{
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80,
	0x03, 0x05, 0x09, 0x11, 0x21, 0x41, 0x81, 0x06, 0x0a, 0x12, 0x22, 0x42, 0x82, 0x0c, 0x18, 0x24,
	0x07, 0x0b, 0x13, 0x23, 0x43, 0x83, 0x0d, 0x19, 0x31, 0x61, 0xc1, 0x0e, 0x1c, 0x38, 0x70, 0xe0,
	0x0f, 0x17, 0x27, 0x47, 0x87, 0x1b, 0x33, 0x63, 0xc3, 0x1d, 0x39, 0x71, 0xe1, 0x3c, 0x78, 0xf0,
	0xf8, 0xf4, 0xec, 0xdc, 0xbc, 0x7c, 0xf2, 0xe6, 0xce, 0x9e, 0x3e, 0xf1, 0xe3, 0xc7, 0x8f, 0x1f,
	0xfc, 0xfa, 0xf6, 0xee, 0xde, 0xbe, 0x7e, 0xf9, 0xf5, 0xed, 0xdd, 0xbd, 0x7d, 0xf3, 0xe7, 0xdb,
	0xfe, 0xfd, 0xfb, 0xf7, 0xef, 0xdf, 0xbf, 0x7f, 0xfe, 0xfd, 0xfb, 0xf7, 0xef, 0xdf, 0xbf, 0x7f,
}

func snellV6SplitMix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

func snellV6PRF32Fold(namespace uint64, label uint32, a uint64, b uint64) uint32 {
	x := namespace ^ (b*snellV6CoefB + snellV6AddB) ^ (uint64(label) * snellV6GoldenGamma) ^ (a*snellV6CoefA + snellV6AddA)
	y := snellV6SplitMix64(x)
	return uint32(y ^ (y >> 32))
}

func snellV6Pick(raw uint32, lo int, hi int) int {
	return lo + int(uint64(raw)%uint64(hi-lo+1))
}

func snellV6PickU32(raw uint32, lo uint32, hi uint32) uint32 {
	return lo + raw%(hi-lo+1)
}

type snellV6Namespaces struct {
	profile, prefix, motif, salt, mix, chunk, write uint64
}

func snellV6DeriveNamespace(secret []byte, label uint32, seedConst uint64) uint64 {
	s0 := binary.LittleEndian.Uint64(secret[0:])
	s1 := binary.LittleEndian.Uint64(secret[8:])
	s2 := binary.LittleEndian.Uint64(secret[16:])
	s3 := binary.LittleEndian.Uint64(secret[24:])
	mixed := uint64(label)*snellV6DomainMul ^
		(seedConst + snellV6NamespaceAdd) ^
		s0 ^
		(s1 + snellV6GoldenGamma) ^
		bits.RotateLeft64(s2, 17) ^
		bits.RotateLeft64(s3, -11)
	return snellV6SplitMix64(mixed)
}

func (n snellV6Namespaces) forLabel(label uint32) uint64 {
	switch label {
	case snellV6LabelPadding, snellV6LabelBitPercent, snellV6LabelPrefixMin, snellV6LabelPrefixMax, snellV6LabelRecordPrefix, snellV6LabelPayloadPad:
		return n.prefix
	case snellV6LabelMotif:
		return n.motif
	case snellV6LabelMixOffset, snellV6LabelMixMode, snellV6LabelMixRounds, snellV6LabelMixStride, snellV6LabelMixOffsetBase, snellV6LabelMixBlock:
		return n.mix
	case snellV6LabelChunkPolicy, snellV6LabelChunkInitial, snellV6LabelChunkMax, snellV6LabelChunkStep, snellV6LabelChunkJitter, snellV6LabelChunkBucket, snellV6LabelChunkSize, snellV6LabelChunkJitterV:
		return n.chunk
	case snellV6LabelWritePolicy, snellV6LabelWriteFirst, snellV6LabelWriteBucket, snellV6LabelWriteSeq, snellV6LabelWriteJitter, snellV6LabelWriteTarget, snellV6LabelWriteJitterV, snellV6LabelWriteNext:
		return n.write
	default:
		return n.profile
	}
}

func (n snellV6Namespaces) prf32(label uint32, a uint32, b uint32) uint32 {
	return snellV6PRF32Fold(n.forLabel(label), label, uint64(a), uint64(b))
}

func (n snellV6Namespaces) prfStatic(label uint32, domain uint32) uint32 {
	return snellV6PRF32Fold(n.forLabel(label), label, 0, uint64(domain))
}

func (n snellV6Namespaces) prfBytes(label uint32, a uint32, output []byte) {
	seed := n.forLabel(label) ^
		(uint64(a)*snellV6DomainMul + 0xb57de1f3f82cb33f) ^
		(uint64(label) * 0xa24baed4963ee407) ^
		(uint64(len(output))*0x165667b19e3779f9 + 0x0d4cd3e7b14a36d7)
	offset := 0
	for offset < len(output) {
		seed += snellV6GoldenGamma
		block := snellV6SplitMix64(seed)
		for shift := 0; shift < 64 && offset < len(output); shift += 8 {
			output[offset] = byte(block >> shift)
			offset++
		}
	}
}

// snellV6Profile holds the PSK-derived values that shape the first record.
type snellV6Profile struct {
	ns snellV6Namespaces

	generator       uint32
	padMin, padMax  int
	padCount        uint32
	padInterval     uint32
	smallLimit      int
	bitMin, bitMax  uint32
	prefixMinRecord int
	prefixMaxRecord int
	chunkPolicy     uint32
	chunkInitial    int
	firstRecordCap  int
	chunkMax        int
	chunkJitter     int
	writePolicy     uint32
	writeFirst      uint32
	chunkBuckets    [8]int
	writeBuckets    [8]int
	writeSeq        [8]int
	writeJitter     int
	writeJitterPct  int
	g1, g2, g3      int
	g4, g5, g6      int

	saltBlockLen       int
	mixRoundsHandshake uint32
}

func newSnellV6Profile(psk []byte) *snellV6Profile {
	input := make([]byte, 0, len(snellV6ProfileSeed)+len(psk))
	input = append(input, snellV6ProfileSeed[:]...)
	input = append(input, psk...)
	secret := blake2b.Sum256(input)
	ns := snellV6Namespaces{
		profile: snellV6DeriveNamespace(secret[:], snellV6LabelProfileID, snellV6NSSeedProfile),
		prefix:  snellV6DeriveNamespace(secret[:], snellV6LabelPadding, snellV6NSSeedPrefix),
		motif:   snellV6DeriveNamespace(secret[:], snellV6LabelMotif, snellV6NSSeedMotif),
		salt:    snellV6DeriveNamespace(secret[:], snellV6LabelSalt, snellV6NSSeedSalt),
		mix:     snellV6DeriveNamespace(secret[:], snellV6LabelMixMode, snellV6NSSeedMix),
		chunk:   snellV6DeriveNamespace(secret[:], snellV6LabelChunkPolicy, snellV6NSSeedChunk),
		write:   snellV6DeriveNamespace(secret[:], snellV6LabelWritePolicy, snellV6NSSeedWrite),
	}

	padMin := snellV6Pick(ns.prfStatic(snellV6LabelPadMin, 0), 0x18, 0xa0)
	padMax := min(padMin+snellV6Pick(ns.prfStatic(snellV6LabelPadMax, 0), 0xa0, 0x3c0), snellV6MaxExtraPadding)

	prefixMinHS := snellV6Pick(ns.prfStatic(snellV6LabelPrefixMin, snellV6HandshakeDomain), 0x10, 0x60)
	prefixMaxHS := min(prefixMinHS+snellV6Pick(ns.prfStatic(snellV6LabelPrefixMax, snellV6HandshakeDomain), 0x10, 0xa0), 0x80)
	prefixMinHS = min(prefixMinHS, prefixMaxHS)
	saltPrefixLen := snellV6Pick(ns.prfStatic(snellV6LabelRecordPrefix, snellV6HandshakeDomain), prefixMinHS, prefixMaxHS)

	prefixMinRecord := snellV6Pick(ns.prfStatic(snellV6LabelPrefixMin, 0), 0x08, 0x50)
	prefixMaxRecord := min(prefixMinRecord+snellV6Pick(ns.prfStatic(snellV6LabelPrefixMax, 0), 0x10, 0xa0), 0x80)
	prefixMinRecord = min(prefixMinRecord, prefixMaxRecord)

	chunkInitial := max(0x60, min(snellV6Pick(ns.prfStatic(snellV6LabelChunkInitial, 0), 0x200, 0x05b4), 0x05b4))
	firstRecordCap := max(0x100, min(snellV6Pick(ns.prfStatic(snellV6LabelChunkFirstCap, snellV6ChunkInitialDomain), 0x100, 0x300), min(chunkInitial, 0x300)))
	chunkMax := max(snellV6Pick(ns.prfStatic(snellV6LabelChunkMax, 0), 0x2000, 0x3fff), chunkInitial)

	p := &snellV6Profile{
		ns:                 ns,
		generator:          ns.prfStatic(snellV6LabelGenerator, 0) & 3,
		padMin:             padMin,
		padMax:             padMax,
		padCount:           snellV6PickU32(ns.prfStatic(snellV6LabelPadCount, 0), 2, 8),
		padInterval:        snellV6PickU32(ns.prfStatic(snellV6LabelPadInterval, 0), 2, 0x0b),
		smallLimit:         snellV6Pick(ns.prfStatic(snellV6LabelSmallLimit, 0), 0x60, 0x300),
		bitMin:             snellV6PickU32(ns.prfStatic(snellV6LabelBitMin, 0), 0x18, 0x29),
		bitMax:             snellV6PickU32(ns.prfStatic(snellV6LabelBitMax, 0), 0x3a, 0x4c),
		prefixMinRecord:    prefixMinRecord,
		prefixMaxRecord:    prefixMaxRecord,
		chunkPolicy:        ns.prfStatic(snellV6LabelChunkPolicy, 0) % 3,
		chunkInitial:       chunkInitial,
		firstRecordCap:     firstRecordCap,
		chunkMax:           chunkMax,
		chunkJitter:        min(snellV6Pick(ns.prfStatic(snellV6LabelChunkJitter, 0), 0x10, 0xc0), 0x0b6),
		writePolicy:        ns.prfStatic(snellV6LabelWritePolicy, 0) % 3,
		writeFirst:         snellV6PickU32(ns.prfStatic(snellV6LabelWriteFirst, 0), 4, 8),
		writeJitter:        snellV6Pick(ns.prfStatic(snellV6LabelWriteJitter, 0), 0x08, 0x60),
		writeJitterPct:     snellV6Pick(ns.prfStatic(snellV6LabelWritePolicy, 0x504c), 8, 0x30),
		g1:                 snellV6Pick(ns.prfStatic(snellV6LabelGenerator, 1), 0x18, 0x80),
		g2:                 snellV6Pick(ns.prfStatic(snellV6LabelGenerator, 2), 0x10, 0x60),
		g3:                 snellV6Pick(ns.prfStatic(snellV6LabelGenerator, 3), 0x10, 0x60),
		g4:                 snellV6Pick(ns.prfStatic(snellV6LabelGenerator, 4), 0x00, 0x09),
		g5:                 snellV6Pick(ns.prfStatic(snellV6LabelGenerator, 5), 0x01, 0x08),
		g6:                 snellV6Pick(ns.prfStatic(snellV6LabelGenerator, 6), 0x07, 0x17),
		saltBlockLen:       snellV6SaltLen + saltPrefixLen,
		mixRoundsHandshake: snellV6PickU32(ns.prfStatic(snellV6LabelMixRounds, snellV6MixHandshakeDomain), 1, 4),
	}
	for index := range 8 {
		p.chunkBuckets[index] = snellV6Pick(ns.prfStatic(snellV6LabelChunkBucket, uint32(index)), 0x1000, chunkMax)
		p.writeBuckets[index] = snellV6Pick(ns.prfStatic(snellV6LabelWriteBucket, uint32(index)), 0x140, 0x05b4)
		p.writeSeq[index] = snellV6Pick(ns.prfStatic(snellV6LabelWriteSeq, uint32(index)), 0x168, 0x05b4)
	}
	return p
}

func (p *snellV6Profile) recordPrefixLen(seq uint32) int {
	return snellV6Pick(p.ns.prf32(snellV6LabelRecordPrefix, seq, 0), p.prefixMinRecord, p.prefixMaxRecord)
}

func (p *snellV6Profile) chunkPayloadLimit(seq uint32, chunkSize int) int {
	switch p.chunkPolicy {
	case 1:
		chunkSize = p.chunkBuckets[p.ns.prf32(snellV6LabelChunkSize, seq, uint32(chunkSize))%uint32(len(p.chunkBuckets))]
	case 2:
		rawJitter := p.ns.prf32(snellV6LabelChunkJitterV, seq, uint32(chunkSize))
		chunkSize += int(rawJitter%uint32(p.chunkJitter*2+1)) - p.chunkJitter
	}
	return max(0x40, min(chunkSize, p.chunkMax))
}

// firstPayloadCap is how much request data the first record carries.
func (p *snellV6Profile) firstPayloadCap() int {
	return max(1, min(p.chunkPayloadLimit(0, p.chunkInitial), p.firstRecordCap, snellV6MaxPayload))
}

func (p *snellV6Profile) paddingLen(seq uint32, payloadLen int, prefixLen int, saltPrefixLen int, totalLen int) int {
	paddingLen := 0
	if seq < p.padCount || (payloadLen > 0 && payloadLen <= p.smallLimit) || (p.padInterval > 0 && seq%p.padInterval == 0) {
		paddingLen = snellV6Pick(p.ns.prf32(snellV6LabelPayloadPad, seq, uint32(payloadLen)), p.padMin, p.padMax)
	}
	payloadTagLen := 0
	if payloadLen > 0 {
		payloadTagLen = snellV6AEADTagLen
	}
	frameLen := totalLen + prefixLen + snellV6HeaderCipherLen + paddingLen + payloadLen + payloadTagLen
	targetLen := p.writeTarget(seq, frameLen)
	if targetLen > frameLen {
		paddingLen += min(targetLen-frameLen, snellV6MaxExtraPadding)
	}
	if totalLen > 0 {
		paddingLen = p.firstRecordPaddingLen(paddingLen, prefixLen, saltPrefixLen, payloadLen)
	}
	return min(paddingLen, 0xffff)
}

func (p *snellV6Profile) writeTarget(seq uint32, frameLen int) int {
	if frameLen > 0x05b3 {
		return min(frameLen, 0xffff)
	}
	var targetLen int
	if seq < p.writeFirst {
		targetLen = p.writeSeq[seq]
	} else {
		targetLen = p.writeBuckets[p.ns.prf32(snellV6LabelWriteTarget, seq, uint32(frameLen))%uint32(len(p.writeBuckets))]
	}
	if p.writePolicy == 2 {
		rawJitter := p.ns.prf32(snellV6LabelWriteJitterV, seq, 0)
		jitter := int(rawJitter%uint32(p.writeJitter*2+1)) - p.writeJitter
		targetLen = max(targetLen+jitter, 1)
	}
	spread := min(snellV6MaxExtraPadding, int(uint64(frameLen)*uint64(p.writeJitterPct)/100))
	if p.ns.prf32(snellV6LabelWriteTarget, seq, uint32(spread))&1 == 0 {
		if targetLen+spread <= 0xffff {
			targetLen += spread
		} else {
			targetLen = 0xffff
		}
	} else {
		halfSpread := spread >> 1
		if targetLen > halfSpread {
			targetLen -= halfSpread
		}
	}
	for targetLen >= 0 && frameLen > targetLen {
		nextLen := p.writeBuckets[p.ns.prf32(snellV6LabelWriteNext, seq, uint32(targetLen))%uint32(len(p.writeBuckets))]
		if nextLen <= targetLen {
			nextLen = targetLen + p.padMax
			if nextLen > 0xffff {
				return 0xffff
			}
		}
		targetLen = nextLen
	}
	return targetLen
}

func (p *snellV6Profile) firstRecordPaddingLen(paddingLen int, prefixLen int, saltPrefixLen int, payloadLen int) int {
	overheadLen := saltPrefixLen + prefixLen + paddingLen
	thresholdInput := payloadLen + 0x37
	if payloadLen == 0 {
		thresholdInput = payloadLen + 0x27
	}
	threshold := max((thresholdInput*25+0x4a)/75, 0xc0)
	if overheadLen >= threshold {
		return paddingLen
	}
	targetPaddingLen := threshold - saltPrefixLen - prefixLen
	maxPaddingLen := p.padMax + snellV6MaxExtraPadding
	if maxPaddingLen <= targetPaddingLen {
		targetPaddingLen = maxPaddingLen
	}
	if targetPaddingLen > 0xfffe {
		return paddingLen
	}
	return targetPaddingLen
}

func (p *snellV6Profile) fillPadding(seq uint32, output []byte) {
	if len(output) == 0 {
		return
	}
	p.ns.prfBytes(snellV6LabelPadding, seq, output)
	switch p.generator {
	case 0:
		bitCount := snellV6PickU32(p.ns.prf32(snellV6LabelBitPercent, seq, 0), p.bitMin, p.bitMax)
		rotate := 1
		scaled := int(bitCount) * 8
		if scaled > 0x31 {
			rotate = 7
			if scaled <= 0x02ed {
				rotate = (scaled + 0x32) / 100
			}
		}
		for index, value := range output {
			raw := byte(int(value) + index)
			tableValue := snellV6BitRotateTable[max(rotate, 1)*16+int((raw^value)&0x0f)]
			output[index] = bits.RotateLeft8(tableValue, int((raw^(value>>4))&7))
		}
	case 1:
		for index, value := range output {
			bucket := int(value) % (p.g1 + p.g2 + p.g3)
			switch {
			case bucket < p.g1:
				output[index] = byte(snellV6Pick(uint32(byte(int(value)+index)), 0x20, 0x7e))
			case bucket < p.g1+p.g2:
				output[index] = byte(snellV6Pick(uint32(byte(int(value)^index)), 0x80, 0xbf))
			default:
				output[index] = byte(snellV6Pick(uint32(byte(int(value)+index*7)), 0xc0, 0xff))
			}
		}
	case 2:
		for index, value := range output {
			low := (int(value&0x0f) + p.g4 + (index & 1)) % 10
			high := (int(value) + ((index & 3) << 4) + 0x30) & 0xf0
			output[index] = byte(high | low)
		}
	case 3:
		motif := make([]byte, 32)
		p.ns.prfBytes(snellV6LabelMotif, seq, motif)
		motifLen := p.g5 * 4
		if motifLen == 0 {
			motifLen = 4
		}
		period := max(p.g6, 5)
		blockOffset := 0
		motifOffset := 0
		for index, value := range output {
			switch {
			case blockOffset < period-3:
				output[index] = byte((p.g5+3)*index) ^ motif[motifOffset%len(motif)]
			case blockOffset < period-1:
				output[index] = byte(0x30 | (int(value) % 10))
			}
			blockOffset++
			if blockOffset == period {
				blockOffset = 0
			}
			motifOffset++
			if motifOffset == motifLen {
				motifOffset = 0
			}
		}
	}
}

func (p *snellV6Profile) saltPositions() []byte {
	length := p.saltBlockLen
	out := make([]byte, length)
	for index := range out {
		out[index] = byte(index)
	}
	rounds := max(p.mixRoundsHandshake, 1)
	for round := uint32(0); round < rounds; round++ {
		domain := uint32(snellV6MixHandshakeDomain) + round
		for i := range length {
			rdx := uint64(i)*snellV6CoefB + snellV6AddB
			rsi := uint64(domain)*snellV6CoefA + snellV6AddA
			y := snellV6SplitMix64((rdx ^ (p.ns.salt ^ snellV6SaltNSXor)) ^ rsi)
			raw := uint64(uint32(y ^ (y >> 32)))
			j := i + int(raw%uint64(length-i))
			out[i], out[j] = out[j], out[i]
		}
	}
	return out[:snellV6SaltLen]
}

// snellV6FirstRecord is the wire layout of the first default-mode record for
// one request size. Salt, sealed header and sealed payload are uniformly
// random; every other byte, and so FixedOnes, is fixed by the PSK. Mixing
// swaps bytes inside the record and does not change the count.
type snellV6FirstRecord struct {
	PayloadLen int `json:"payload_len"`
	TotalLen   int `json:"total_len"`
	FixedOnes  int `json:"fixed_ones"`
	RandomLen  int `json:"random_len"`
}

// Density is the expected share of one bits on the wire.
func (r snellV6FirstRecord) Density() float64 {
	return float64(2*r.FixedOnes+8*r.RandomLen) / float64(16*r.TotalLen)
}

func (p *snellV6Profile) firstRecord(requested int) snellV6FirstRecord {
	payloadLen := min(requested, p.firstPayloadCap())
	prefixLen := p.recordPrefixLen(0)
	saltPrefixLen := p.saltBlockLen - snellV6SaltLen
	paddingLen := p.paddingLen(0, payloadLen, prefixLen, saltPrefixLen, p.saltBlockLen)
	fixed := make([]byte, max(p.saltBlockLen, prefixLen+paddingLen))
	block := fixed[:p.saltBlockLen]
	p.fillPadding(0xffffffff, block)
	for _, position := range p.saltPositions() {
		block[position] = 0
	}
	ones := 0
	for _, b := range block {
		ones += bits.OnesCount8(b)
	}
	rest := fixed[:prefixLen+paddingLen]
	p.fillPadding(0, rest[:prefixLen])
	p.fillPadding(0, rest[prefixLen:])
	for _, b := range rest {
		ones += bits.OnesCount8(b)
	}
	payloadCipherLen := 0
	if payloadLen > 0 {
		payloadCipherLen = payloadLen + snellV6AEADTagLen
	}
	return snellV6FirstRecord{
		PayloadLen: payloadLen,
		TotalLen:   p.saltBlockLen + prefixLen + snellV6HeaderCipherLen + paddingLen + payloadCipherLen,
		FixedOnes:  ones,
		RandomLen:  snellV6SaltLen + snellV6HeaderCipherLen + payloadCipherLen,
	}
}
