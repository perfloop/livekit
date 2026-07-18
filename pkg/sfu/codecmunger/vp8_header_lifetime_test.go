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

package codecmunger

import (
	"bytes"
	"testing"

	"github.com/pion/rtp"
	"github.com/stretchr/testify/require"

	"github.com/livekit/mediatransportutil/pkg/codec"
	"github.com/livekit/protocol/logger"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
)

func newVP8HeaderLifetimePacket(t testing.TB, pictureID uint16) *buffer.ExtPacket {
	t.Helper()

	vp8 := codec.VP8{
		FirstByte:  0x10,
		I:          true,
		M:          true,
		PictureID:  pictureID,
		L:          true,
		TL0PICIDX:  23,
		T:          true,
		TID:        0,
		Y:          true,
		K:          true,
		KEYIDX:     7,
		HeaderSize: 6,
		IsKeyFrame: true,
	}
	inputHeader, err := vp8.Marshal()
	require.NoError(t, err)

	payload := make([]byte, len(inputHeader)+1200)
	copy(payload, inputHeader)
	for i := range payload[len(inputHeader):] {
		payload[len(inputHeader)+i] = byte(i)
	}

	return &buffer.ExtPacket{
		Packet: &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				SequenceNumber: pictureID,
				Timestamp:      uint32(pictureID) * 3_000,
				SSRC:           0x12345678,
			},
			Payload: payload,
		},
		ExtSequenceNumber: uint64(pictureID),
		ExtTimestamp:      uint64(pictureID) * 3_000,
		Payload:           vp8,
	}
}

func copyVP8HeaderIntoForwardedPayload(dst, header []byte, extPkt *buffer.ExtPacket, incomingHeaderSize int) ([]byte, bool) {
	if incomingHeaderSize < 0 || incomingHeaderSize > len(extPkt.Packet.Payload) {
		return nil, false
	}

	media := extPkt.Packet.Payload[incomingHeaderSize:]
	if len(header)+len(media) > len(dst) {
		return nil, false
	}

	n := copy(dst, header)
	n += copy(dst[n:], media)
	forwarded := dst[:n]
	return forwarded, n == len(header)+len(media) &&
		bytes.Equal(forwarded[:len(header)], header) &&
		bytes.Equal(forwarded[len(header):], media)
}

func TestUpdateAndGetRetainsPreviousHeader(t *testing.T) {
	munger := NewVP8(logger.GetLogger())
	outboundPayload := make([]byte, 1500)

	firstPacket := newVP8HeaderLifetimePacket(t, 0x100)
	inputHeaderSize, firstHeader, err := munger.UpdateAndGet(firstPacket, false, false, 0)
	require.NoError(t, err)
	firstHeaderSnapshot := append([]byte(nil), firstHeader...)
	_, copied := copyVP8HeaderIntoForwardedPayload(outboundPayload, firstHeader, firstPacket, inputHeaderSize)
	require.True(t, copied)

	for _, pictureID := range []uint16{0x101, 0x102} {
		extPkt := newVP8HeaderLifetimePacket(t, pictureID)
		inputHeaderSize, header, err := munger.UpdateAndGet(extPkt, false, false, 0)
		require.NoError(t, err)
		_, copied = copyVP8HeaderIntoForwardedPayload(outboundPayload, header, extPkt, inputHeaderSize)
		require.True(t, copied)
	}

	require.Equal(t, firstHeaderSnapshot, firstHeader)
}
