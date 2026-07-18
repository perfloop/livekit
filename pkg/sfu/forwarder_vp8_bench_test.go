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
	"github.com/livekit/livekit-server/pkg/sfu/codecmunger"
	"github.com/livekit/livekit-server/pkg/sfu/testutils"
	"github.com/livekit/mediatransportutil/pkg/codec"
	"github.com/livekit/protocol/logger"
)

func newVP8BenchmarkPacket(t testing.TB, sequenceNumber uint16, pictureID uint16, isKeyFrame bool) *buffer.ExtPacket {
	t.Helper()

	vp8 := &codec.VP8{
		FirstByte:  0x10,
		I:          true,
		M:          true,
		PictureID:  pictureID,
		L:          true,
		TL0PICIDX:  uint8(pictureID),
		T:          true,
		TID:        0,
		Y:          true,
		K:          true,
		KEYIDX:     uint8(pictureID),
		HeaderSize: 6,
		IsKeyFrame: isKeyFrame,
	}

	extPkt, err := testutils.GetTestExtPacketVP8(&testutils.TestExtPacketParams{
		Marker:         true,
		SequenceNumber: sequenceNumber,
		Timestamp:      uint32(sequenceNumber) * 3000,
		SSRC:           1,
		PayloadSize:    20,
	}, vp8)
	require.NoError(t, err)
	return extPkt
}

func TestForwarderGetTranslationParamsKeepsVP8Header(t *testing.T) {
	f := newForwarder(testutils.TestVP8Codec, webrtc.RTPCodecTypeVideo)
	f.vls.SetTarget(buffer.VideoLayer{Spatial: 0, Temporal: 0})
	f.vls.SetCurrent(buffer.InvalidLayer)

	first, err := f.GetTranslationParams(newVP8BenchmarkPacket(t, 1, 128, true), 0)
	require.NoError(t, err)
	require.False(t, first.shouldDrop)
	firstHeader := append([]byte(nil), first.codecBytes[:first.numCodecBytes]...)

	second, err := f.GetTranslationParams(newVP8BenchmarkPacket(t, 2, 129, false), 0)
	require.NoError(t, err)
	require.False(t, second.shouldDrop)

	require.Equal(t, firstHeader, first.codecBytes[:first.numCodecBytes])
	require.NotEqual(t, first.codecBytes[:first.numCodecBytes], second.codecBytes[:second.numCodecBytes])
}

func BenchmarkVP8UpdateAndGet(b *testing.B) {
	packets := [2]*buffer.ExtPacket{
		newVP8BenchmarkPacket(b, 1, 128, true),
		newVP8BenchmarkPacket(b, 2, 129, false),
	}

	v := codecmunger.NewVP8(logger.GetLogger())
	var checksum uint64

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		inputSize, header, err := v.UpdateAndGet(packets[i&1], false, false, 0)
		if err != nil {
			b.Fatal(err)
		}
		if inputSize != 6 || len(header) != 6 {
			b.Fatalf("unexpected VP8 header sizes: input=%d output=%d", inputSize, len(header))
		}

		for _, b := range header {
			checksum += uint64(b)
		}
	}

	if checksum == 0 {
		b.Fatal("VP8 headers were not consumed")
	}
}
