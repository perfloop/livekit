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
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/livekit-server/pkg/sfu/pacer"
	"github.com/livekit/livekit-server/pkg/sfu/rtpstats"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

const (
	headerPoolLifecyclePayloadType   = 111
	headerPoolLifecycleAbsSendTimeID = 1
)

var _ webrtc.TrackLocalWriter = (*headerPoolLifecycleWriter)(nil)
var _ DownTrackListener = (*headerPoolLifecycleListener)(nil)

type headerPoolLifecycleListener struct{}

func (*headerPoolLifecycleListener) OnBindAndConnected() {}

func (*headerPoolLifecycleListener) OnStatsUpdate(_ *livekit.AnalyticsStat) {}

func (*headerPoolLifecycleListener) OnMaxSubscribedLayerChanged(_ int32) {}

func (*headerPoolLifecycleListener) OnRttUpdate(_ uint32) {}

func (*headerPoolLifecycleListener) OnCodecNegotiated(_ webrtc.RTPCodecCapability) {}

func (*headerPoolLifecycleListener) OnDownTrackClose(_ bool) {}

func (*headerPoolLifecycleListener) OnStreamStarted() {}

type headerPoolLifecycleWriter struct {
	extensionLengths []int
	lastPayload      []byte
}

func (w *headerPoolLifecycleWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	w.extensionLengths = append(w.extensionLengths, len(header.GetExtension(headerPoolLifecycleAbsSendTimeID)))
	w.lastPayload = append(w.lastPayload[:0], payload...)
	return header.MarshalSize() + len(payload), nil
}

func (w *headerPoolLifecycleWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func newHeaderPoolLifecycleDownTrack(tb testing.TB, writer *headerPoolLifecycleWriter) (*DownTrack, *buffer.ExtPacket) {
	tb.Helper()

	stats := rtpstats.NewRTPStatsSender(rtpstats.RTPStatsParams{}, 128)
	stats.SetClockRate(48000)
	passThrough := pacer.NewPassThrough(logger.GetLogger(), nil)
	forwarder := NewForwarder(
		webrtc.RTPCodecTypeAudio,
		logger.GetLogger(),
		true, // skipReferenceTS
		true, // disableOpportunisticAllocation
		false,
		stats,
	)
	forwarder.DetermineCodec(
		webrtc.RTPCodecCapability{
			MimeType:  "audio/opus",
			ClockRate: 48000,
		},
		nil,
		livekit.VideoLayer_MODE_UNUSED,
	)

	downTrack := &DownTrack{
		params: DownTrackParams{
			Listener: &headerPoolLifecycleListener{},
			Logger:   logger.GetLogger(),
			Pacer:    passThrough,
		},
		ssrc:             0x11223344,
		forwarder:        forwarder,
		rtpStats:         stats,
		pacer:            passThrough,
		writeStream:      writer,
		absSendTimeExtID: headerPoolLifecycleAbsSendTimeID,
	}
	downTrack.payloadType.Store(headerPoolLifecyclePayloadType)
	downTrack.writable.Store(true)

	extPkt := &buffer.ExtPacket{
		Packet: &rtp.Packet{
			Header: rtp.Header{
				Version:     2,
				PayloadType: headerPoolLifecyclePayloadType,
				SSRC:        0x55667788,
			},
			Payload: make([]byte, 128),
		},
	}
	return downTrack, extPkt
}

func TestDownTrackWriteRTPHeaderPoolLifecycle(t *testing.T) {
	originalFactory := RTPHeaderFactory
	RTPHeaderFactory = &sync.Pool{
		New: func() any {
			return &rtp.Header{}
		},
	}
	t.Cleanup(func() {
		RTPHeaderFactory = originalFactory
	})

	writer := &headerPoolLifecycleWriter{}
	downTrack, extPkt := newHeaderPoolLifecycleDownTrack(t, writer)
	writeRTP := func(sequenceNumber uint64) {
		extPkt.Packet.SequenceNumber = uint16(sequenceNumber)
		extPkt.Packet.Timestamp = uint32(sequenceNumber * 960)
		extPkt.Packet.Payload[0] = byte(sequenceNumber)
		extPkt.Arrival = int64(sequenceNumber) * 20_000_000
		extPkt.ExtSequenceNumber = sequenceNumber
		extPkt.ExtTimestamp = sequenceNumber * 960

		require.Equal(t, int32(1), downTrack.WriteRTP(extPkt, 0))
	}

	writeRTP(1)
	// A sync.Pool may discard a returned header during GC. Both a fresh header
	// and a retained one must complete the next send with a patched extension.
	runtime.GC()
	writeRTP(2)
	writeRTP(3)

	require.Equal(t, []int{3, 3, 3}, writer.extensionLengths)
	require.Equal(t, byte(3), writer.lastPayload[0])
}
