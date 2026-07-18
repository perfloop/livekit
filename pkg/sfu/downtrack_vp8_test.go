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
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/require"

	"github.com/livekit/mediatransportutil/pkg/codec"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/livekit-server/pkg/sfu/pacer"
	"github.com/livekit/livekit-server/pkg/sfu/rtpstats"
	"github.com/livekit/livekit-server/pkg/sfu/testutils"
)

type downTrackVP8TestListener struct{}

func (downTrackVP8TestListener) OnBindAndConnected() {}

func (downTrackVP8TestListener) OnStatsUpdate(*livekit.AnalyticsStat) {}

func (downTrackVP8TestListener) OnMaxSubscribedLayerChanged(int32) {}

func (downTrackVP8TestListener) OnRttUpdate(uint32) {}

func (downTrackVP8TestListener) OnCodecNegotiated(webrtc.RTPCodecCapability) {}

func (downTrackVP8TestListener) OnDownTrackClose(bool) {}

func (downTrackVP8TestListener) OnStreamStarted() {}

type downTrackVP8CaptureWriter struct {
	payload []byte
}

func (w *downTrackVP8CaptureWriter) WriteRTP(_ *rtp.Header, payload []byte) (int, error) {
	w.payload = append(w.payload[:0], payload...)
	return len(payload), nil
}

func (w *downTrackVP8CaptureWriter) Write(payload []byte) (int, error) {
	return len(payload), nil
}

func TestDownTrackWriteRTPUsesVP8HeaderLength(t *testing.T) {
	inputDescriptor := codec.VP8{
		FirstByte:  0x10,
		I:          true,
		M:          true,
		PictureID:  127,
		L:          true,
		TL0PICIDX:  9,
		T:          true,
		TID:        0,
		Y:          true,
		K:          true,
		KEYIDX:     3,
		HeaderSize: 6,
		IsKeyFrame: true,
	}
	inputHeader, err := inputDescriptor.Marshal()
	require.NoError(t, err)
	require.Len(t, inputHeader, 6)

	media := []byte{0xde, 0xad, 0xbe, 0xef}
	extPkt, err := testutils.GetTestExtPacketVP8(&testutils.TestExtPacketParams{
		Marker:         true,
		IsKeyFrame:     true,
		PayloadType:    96,
		SequenceNumber: 1000,
		Timestamp:      90000,
		SSRC:           0x12345678,
		PayloadSize:    len(inputHeader) + len(media),
		ArrivalTime:    time.Now(),
		VideoLayer:     buffer.VideoLayer{Spatial: 0, Temporal: 0},
	}, &inputDescriptor)
	require.NoError(t, err)
	extPkt.Packet.Payload = append(inputHeader, media...)

	forwarder := newForwarder(testutils.TestVP8Codec, webrtc.RTPCodecTypeVideo)
	forwarder.vls.SetTarget(buffer.VideoLayer{Spatial: 0, Temporal: 0})
	forwarder.vls.SetCurrent(buffer.InvalidLayer)
	writer := &downTrackVP8CaptureWriter{}
	downTrack := &DownTrack{
		params: DownTrackParams{
			Logger:   logger.GetLogger(),
			Listener: downTrackVP8TestListener{},
		},
		kind:        webrtc.RTPCodecTypeVideo,
		ssrc:        0x87654321,
		forwarder:   forwarder,
		writeStream: writer,
		pacer:       pacer.NewPassThrough(logger.GetLogger(), nil),
		rtpStats:    rtpstats.NewRTPStatsSender(rtpstats.RTPStatsParams{}, 128),
	}
	downTrack.payloadType.Store(96)
	downTrack.writable.Store(true)

	require.EqualValues(t, 1, downTrack.WriteRTP(extPkt, 0))

	expectedDescriptor := inputDescriptor
	expectedDescriptor.M = false
	expectedDescriptor.HeaderSize = 5
	expectedHeader, err := expectedDescriptor.Marshal()
	require.NoError(t, err)
	require.Len(t, expectedHeader, 5)
	expectedPayload := append(expectedHeader, media...)
	require.Equal(t, expectedPayload, writer.payload)
	require.Len(t, writer.payload, len(expectedHeader)+len(media))
}
