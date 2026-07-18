// Copyright 2026 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sfu

import (
	"bytes"
	"testing"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/livekit/mediatransportutil/pkg/codec"
	"github.com/livekit/protocol/logger"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/livekit-server/pkg/sfu/codecmunger"
	"github.com/livekit/livekit-server/pkg/sfu/testutils"
)

const (
	vp8BenchmarkPictureIDMask    uint64 = (1 << 15) - 1
	vp8BenchmarkPictureIDStart   uint64 = 128
	vp8BenchmarkFrameCount              = int(vp8BenchmarkPictureIDMask + 1)
	vp8BenchmarkKeyFrameInterval uint64 = 32
	vp8BenchmarkSequenceStart    uint64 = 1000
	vp8BenchmarkTimestampStart   uint64 = 90000
	vp8BenchmarkTimestampStep    uint64 = 3000
)

// orderedVP8Descriptor models one complete temporal-layer-zero VP8 frame. The
// PictureID, TL0PICIDX, KEYIDX, RTP sequence number, and RTP timestamp all
// advance in their respective domains. The 15-bit PictureID cycle lets a
// long-running benchmark exercise the normal VP8 wrap behavior without
// regressing the input stream.
func orderedVP8Descriptor(frame uint64) codec.VP8 {
	return codec.VP8{
		FirstByte:  0x10, // start of VP8 partition 0
		I:          true,
		M:          true,
		PictureID:  uint16((vp8BenchmarkPictureIDStart + frame) & vp8BenchmarkPictureIDMask),
		L:          true,
		TL0PICIDX:  uint8(frame),
		T:          true,
		TID:        0,
		Y:          true,
		K:          true,
		KEYIDX:     uint8((frame / vp8BenchmarkKeyFrameInterval) & 0x1f),
		HeaderSize: 6,
		IsKeyFrame: frame%vp8BenchmarkKeyFrameInterval == 0,
	}
}

func newOrderedVP8Packets(tb testing.TB) []*buffer.ExtPacket {
	tb.Helper()

	packets := make([]*buffer.ExtPacket, vp8BenchmarkFrameCount)
	for frame := range vp8BenchmarkFrameCount {
		descriptor := orderedVP8Descriptor(uint64(frame))
		payload := make([]byte, descriptor.HeaderSize+1)
		n, err := descriptor.MarshalTo(payload)
		if err != nil {
			tb.Fatalf("marshal VP8 descriptor for frame %d: %v", frame, err)
		}
		if n != descriptor.HeaderSize {
			tb.Fatalf("marshal VP8 descriptor for frame %d: got %d bytes, want %d", frame, n, descriptor.HeaderSize)
		}
		if !descriptor.IsKeyFrame {
			// The first VP8 payload byte's P bit denotes an interframe.
			payload[n] = 0x01
		}

		packets[frame] = &buffer.ExtPacket{
			VideoLayer: buffer.VideoLayer{Spatial: 0, Temporal: 0},
			Packet: &rtp.Packet{
				Header: rtp.Header{
					Version:     2,
					Marker:      true,
					PayloadType: 96,
					SSRC:        0x12345678,
				},
				Payload: payload,
			},
			Payload:    descriptor,
			IsKeyFrame: descriptor.IsKeyFrame,
		}
		setOrderedVP8PacketTiming(packets[frame], uint64(frame))
	}

	return packets
}

func setOrderedVP8PacketTiming(packet *buffer.ExtPacket, frame uint64) {
	sequenceNumber := vp8BenchmarkSequenceStart + frame
	timestamp := vp8BenchmarkTimestampStart + frame*vp8BenchmarkTimestampStep

	packet.Packet.SequenceNumber = uint16(sequenceNumber)
	packet.Packet.Timestamp = uint32(timestamp)
	packet.ExtSequenceNumber = sequenceNumber
	packet.ExtTimestamp = timestamp
}

func expectedMungedVP8Header(tb testing.TB, frame uint64) []byte {
	tb.Helper()

	descriptor := orderedVP8Descriptor(frame)
	// VP8 emits a seven-bit PictureID only for the low 128 values after a
	// natural 15-bit wrap. All input descriptors remain in the six-byte form.
	descriptor.M = descriptor.PictureID > 127
	if descriptor.M {
		descriptor.HeaderSize = 6
	} else {
		descriptor.HeaderSize = 5
	}

	header, err := descriptor.Marshal()
	if err != nil {
		tb.Fatalf("marshal expected VP8 header for frame %d: %v", frame, err)
	}
	return header
}

func requireMungedVP8Header(tb testing.TB, inputSize int, header []byte, frame uint64) {
	tb.Helper()

	if inputSize != 6 {
		tb.Fatalf("frame %d consumed %d-byte VP8 header, want 6", frame, inputSize)
	}
	if expected := expectedMungedVP8Header(tb, frame); !bytes.Equal(header, expected) {
		tb.Fatalf("frame %d munged VP8 header = %x, want %x", frame, header, expected)
	}
}

func newVP8BenchmarkForwarder() *Forwarder {
	forwarder := newForwarder(testutils.TestVP8Codec, webrtc.RTPCodecTypeVideo)
	forwarder.vls.SetTarget(buffer.VideoLayer{Spatial: 0, Temporal: 0})
	forwarder.vls.SetCurrent(buffer.InvalidLayer)
	return forwarder
}

func requireForwardedVP8Header(tb testing.TB, params TranslationParams, frame uint64) {
	tb.Helper()

	if params.shouldDrop {
		tb.Fatalf("frame %d was unexpectedly dropped", frame)
	}
	expected := expectedMungedVP8Header(tb, frame)
	if params.incomingHeaderSize != 6 {
		tb.Fatalf("frame %d consumed %d-byte VP8 header, want 6", frame, params.incomingHeaderSize)
	}
	if len(params.codecBytes) < len(expected) {
		tb.Fatalf("frame %d produced %d codec bytes, want at least %d", frame, len(params.codecBytes), len(expected))
	}
	if actual := params.codecBytes[:len(expected)]; !bytes.Equal(actual, expected) {
		tb.Fatalf("frame %d translated VP8 header = %x, want %x", frame, actual, expected)
	}
}

// BenchmarkVP8UpdateAndGet measures a steady-state ordered VP8 stream. Each
// operation is a complete marker-delimited frame, not a replayed descriptor:
// the benchmark advances RTP and VP8 frame identifiers before every call.
func BenchmarkVP8UpdateAndGet(b *testing.B) {
	packets := newOrderedVP8Packets(b)
	munger := codecmunger.NewVP8(logger.GetLogger())

	var (
		frame     uint64
		inputSize int
		header    []byte
		err       error
	)
	inputSize, header, err = munger.UpdateAndGet(packets[frame], false, false, 0)
	if err != nil {
		b.Fatal(err)
	}
	requireMungedVP8Header(b, inputSize, header, frame)

	for b.Loop() {
		frame++
		packet := packets[frame&vp8BenchmarkPictureIDMask]
		setOrderedVP8PacketTiming(packet, frame)
		inputSize, header, err = munger.UpdateAndGet(packet, false, false, 0)
	}
	if err != nil {
		b.Fatal(err)
	}
	requireMungedVP8Header(b, inputSize, header, frame)
}

// TestForwarderVP8TranslationParamsHeaderLifetime verifies that the translated
// header returned by value remains valid until its caller consumes it. The
// forwarding path can invoke both padding and a later packet translation after
// the first GetTranslationParams call, so a shared munger scratch buffer must
// not overwrite the first result.
func TestForwarderVP8TranslationParamsHeaderLifetime(t *testing.T) {
	packets := newOrderedVP8Packets(t)
	forwarder := newVP8BenchmarkForwarder()

	params0, err := forwarder.GetTranslationParams(packets[0], 0)
	if err != nil {
		t.Fatal(err)
	}
	requireForwardedVP8Header(t, params0, 0)
	firstHeader := append([]byte(nil), params0.codecBytes[:len(expectedMungedVP8Header(t, 0))]...)

	padding, err := forwarder.GetPadding(true)
	if err != nil {
		t.Fatal(err)
	}
	if expected := expectedMungedVP8Header(t, 0); !bytes.Equal(padding, expected) {
		t.Fatalf("VP8 padding header = %x, want %x", padding, expected)
	}
	if actual := params0.codecBytes[:len(firstHeader)]; !bytes.Equal(actual, firstHeader) {
		t.Fatalf("first translated VP8 header changed after padding: got %x, want %x", actual, firstHeader)
	}

	params1, err := forwarder.GetTranslationParams(packets[1], 0)
	if err != nil {
		t.Fatal(err)
	}
	requireForwardedVP8Header(t, params1, 1)
	if actual := params0.codecBytes[:len(firstHeader)]; !bytes.Equal(actual, firstHeader) {
		t.Fatalf("first translated VP8 header changed after the next packet: got %x, want %x", actual, firstHeader)
	}
}
