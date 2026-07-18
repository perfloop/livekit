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
	"testing"

	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/require"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/livekit-server/pkg/sfu/testutils"
)

func setForwarderBenchmarkPacketSequence(extPkt *buffer.ExtPacket, sequenceNumber uint64) {
	extPkt.Packet.SequenceNumber = uint16(sequenceNumber)
	extPkt.ExtSequenceNumber = sequenceNumber
	extPkt.Packet.Timestamp = uint32(sequenceNumber * 3000)
	extPkt.ExtTimestamp = sequenceNumber * 3000
}

func BenchmarkForwarderGetTranslationParamsVP8(b *testing.B) {
	f := newForwarder(testutils.TestVP8Codec, webrtc.RTPCodecTypeVideo)
	f.vls.SetTarget(buffer.VideoLayer{Spatial: 0, Temporal: 0})
	f.vls.SetCurrent(buffer.InvalidLayer)

	extPkt := newVP8BenchmarkPacket(b, 1, 128, true)
	_, err := f.GetTranslationParams(extPkt, 0)
	require.NoError(b, err)

	var checksum uint64
	b.ReportAllocs()
	b.ResetTimer()
	for sequenceNumber := uint64(2); b.Loop(); sequenceNumber++ {
		setForwarderBenchmarkPacketSequence(extPkt, sequenceNumber)
		tp, err := f.GetTranslationParams(extPkt, 0)
		if err != nil {
			b.Fatal(err)
		}
		if tp.shouldDrop || len(tp.codecBytes) == 0 {
			b.Fatal("selected VP8 packet was not translated")
		}
		checksum += tp.rtp.extSequenceNumber + uint64(tp.incomingHeaderSize) + uint64(tp.codecBytes[0])
	}

	if checksum == 0 {
		b.Fatal("translated VP8 headers were not consumed")
	}
}

func BenchmarkForwarderGetTranslationParamsOpus(b *testing.B) {
	f := newForwarder(testutils.TestOpusCodec, webrtc.RTPCodecTypeAudio)
	extPkt, err := testutils.GetTestExtPacket(&testutils.TestExtPacketParams{
		Marker:      true,
		PayloadSize: 20,
		SSRC:        1,
	})
	require.NoError(b, err)

	var checksum uint64
	b.ReportAllocs()
	b.ResetTimer()
	for sequenceNumber := uint64(1); b.Loop(); sequenceNumber++ {
		setForwarderBenchmarkPacketSequence(extPkt, sequenceNumber)
		tp, err := f.GetTranslationParams(extPkt, 0)
		if err != nil {
			b.Fatal(err)
		}
		if tp.shouldDrop {
			b.Fatal("in-order Opus packet was dropped")
		}
		checksum += tp.rtp.extSequenceNumber + uint64(tp.incomingHeaderSize)
	}

	if checksum == 0 {
		b.Fatal("translated Opus parameters were not consumed")
	}
}
