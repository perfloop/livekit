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
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/livekit-server/pkg/sfu/bwe"
	"github.com/livekit/livekit-server/pkg/sfu/ccutils"
	"github.com/livekit/livekit-server/pkg/sfu/pacer"
	"github.com/livekit/livekit-server/pkg/sfu/rtpstats"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

const headerExtensionPayloadType = 111

var _ bwe.BWE = (*headerExtensionBWE)(nil)
var _ webrtc.TrackLocalWriter = (*headerExtensionWriter)(nil)
var _ DownTrackListener = (*headerExtensionListener)(nil)

type headerExtensionBWE struct {
	bwe.NullBWE

	calls          int
	lastPacketSize int
	nextSequence   uint16
}

func (*headerExtensionBWE) Type() bwe.BWEType {
	return bwe.BWETypeSendSide
}

func (b *headerExtensionBWE) RecordPacketSendAndGetSequenceNumber(
	_atMicro int64,
	size int,
	_isRTX bool,
	_probeClusterID ccutils.ProbeClusterId,
	_isProbe bool,
) uint16 {
	b.calls++
	b.lastPacketSize = size
	b.nextSequence += 37
	return b.nextSequence
}

type headerExtensionWriter struct {
	buf      [1500]byte
	n        int
	checksum uint64
}

func (w *headerExtensionWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	n, err := rtp.MarshalPacketTo(w.buf[:], header, payload)
	if err != nil {
		return 0, err
	}

	w.n = n
	w.checksum += uint64(w.buf[n-1]) + uint64(n)
	return n, nil
}

func (w *headerExtensionWriter) Write(p []byte) (int, error) {
	if len(p) != 0 {
		w.checksum += uint64(p[len(p)-1])
	}
	return len(p), nil
}

func (w *headerExtensionWriter) packet() (*rtp.Packet, error) {
	packet := &rtp.Packet{}
	err := packet.Unmarshal(w.buf[:w.n])
	return packet, err
}

type headerExtensionListener struct{}

func (*headerExtensionListener) OnBindAndConnected() {}

func (*headerExtensionListener) OnStatsUpdate(_ *livekit.AnalyticsStat) {}

func (*headerExtensionListener) OnMaxSubscribedLayerChanged(_ int32) {}

func (*headerExtensionListener) OnRttUpdate(_ uint32) {}

func (*headerExtensionListener) OnCodecNegotiated(_ webrtc.RTPCodecCapability) {}

func (*headerExtensionListener) OnDownTrackClose(_ bool) {}

func (*headerExtensionListener) OnStreamStarted() {}

type headerExtensionAllocator struct{}

func (*headerExtensionAllocator) OnREMB(_ *DownTrack, _ *rtcp.ReceiverEstimatedMaximumBitrate) {}

func (*headerExtensionAllocator) OnTransportCCFeedback(_ *DownTrack, _ *rtcp.TransportLayerCC) {}

func (*headerExtensionAllocator) OnAvailableLayersChanged(_ *DownTrack) {}

func (*headerExtensionAllocator) OnBitrateAvailabilityChanged(_ *DownTrack) {}

func (*headerExtensionAllocator) OnMaxPublishedSpatialChanged(_ *DownTrack) {}

func (*headerExtensionAllocator) OnMaxPublishedTemporalChanged(_ *DownTrack) {}

func (*headerExtensionAllocator) OnSubscriptionChanged(_ *DownTrack) {}

func (*headerExtensionAllocator) OnSubscribedLayerChanged(_ *DownTrack, _ buffer.VideoLayer) {}

func (*headerExtensionAllocator) OnResume(_ *DownTrack) {}

func (*headerExtensionAllocator) IsBWEEnabled(_ *DownTrack) bool { return true }

func (*headerExtensionAllocator) BWEType() bwe.BWEType { return bwe.BWETypeSendSide }

func (*headerExtensionAllocator) IsSubscribeMutable(_ *DownTrack) bool { return true }

var _ DownTrackStreamAllocatorListener = (*headerExtensionAllocator)(nil)

type downTrackHeaderExtensionHarness struct {
	downTrack     *DownTrack
	bwe           *headerExtensionBWE
	packet        *bufferedHeaderExtensionPacket
	transportCCID uint8
	writer        *headerExtensionWriter
}

// bufferedHeaderExtensionPacket keeps the mutable per-packet values outside
// the benchmark loop's setup so every iteration still forwards a distinct RTP
// packet through DownTrack.WriteRTP.
type bufferedHeaderExtensionPacket struct {
	next uint64
	ext  *buffer.ExtPacket
}

func configureSendSideTransportCC(tb testing.TB, downTrack *DownTrack) {
	tb.Helper()

	mediaEngine := &webrtc.MediaEngine{}
	require.NoError(tb, mediaEngine.RegisterDefaultCodecs())
	require.NoError(tb, mediaEngine.RegisterHeaderExtension(
		webrtc.RTPHeaderExtensionCapability{URI: sdp.TransportCCURI},
		webrtc.RTPCodecTypeAudio,
	))

	peerConnection, err := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine)).NewPeerConnection(webrtc.Configuration{})
	require.NoError(tb, err)
	tb.Cleanup(func() {
		require.NoError(tb, peerConnection.Close())
	})

	transceiver, err := peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio)
	require.NoError(tb, err)
	downTrack.transceiver.Store(transceiver)
	downTrack.streamAllocatorListener = &headerExtensionAllocator{}
	downTrack.setRTPHeaderExtensions()

	require.Zero(tb, downTrack.absSendTimeExtID)
	require.NotZero(tb, downTrack.transportWideExtID)
}

func newDownTrackHeaderExtensionHarness(tb testing.TB) *downTrackHeaderExtensionHarness {
	tb.Helper()
	stats := rtpstats.NewRTPStatsSender(rtpstats.RTPStatsParams{}, 128)
	stats.SetClockRate(48000)

	writer := &headerExtensionWriter{}
	estimator := &headerExtensionBWE{nextSequence: 0x9000}
	passThrough := pacer.NewPassThrough(logger.GetLogger(), estimator)
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
			Listener: &headerExtensionListener{},
			Logger:   logger.GetLogger(),
			Pacer:    passThrough,
		},
		ssrc:        0x11223344,
		forwarder:   forwarder,
		rtpStats:    stats,
		pacer:       passThrough,
		writeStream: writer,
	}
	downTrack.payloadType.Store(headerExtensionPayloadType)
	downTrack.writable.Store(true)
	configureSendSideTransportCC(tb, downTrack)

	packet := &bufferedHeaderExtensionPacket{
		next: 1,
		ext: &buffer.ExtPacket{
			Packet: &rtp.Packet{
				Header: rtp.Header{
					Version:     2,
					PayloadType: headerExtensionPayloadType,
					SSRC:        0x55667788,
				},
				Payload: make([]byte, 128),
			},
		},
	}

	return &downTrackHeaderExtensionHarness{
		downTrack:     downTrack,
		bwe:           estimator,
		packet:        packet,
		transportCCID: uint8(downTrack.transportWideExtID),
		writer:        writer,
	}
}

func (h *downTrackHeaderExtensionHarness) writeRTP() int32 {
	sequenceNumber := h.packet.next
	h.packet.next++

	h.packet.ext.Packet.SequenceNumber = uint16(sequenceNumber)
	h.packet.ext.Packet.Timestamp = uint32(sequenceNumber * 960)
	h.packet.ext.Packet.Payload[0] = byte(sequenceNumber)
	h.packet.ext.Arrival = int64(sequenceNumber) * 20_000_000
	h.packet.ext.ExtSequenceNumber = sequenceNumber
	h.packet.ext.ExtTimestamp = sequenceNumber * 960

	return h.downTrack.WriteRTP(h.packet.ext, 0)
}

func isolateRTPHeaderFactory(tb testing.TB) {
	tb.Helper()

	original := RTPHeaderFactory
	RTPHeaderFactory = &sync.Pool{
		New: func() any {
			return &rtp.Header{}
		},
	}
	tb.Cleanup(func() {
		RTPHeaderFactory = original
	})
}

func TestDownTrackWriteRTPHeaderExtensions(t *testing.T) {
	t.Run("send-side transport-cc is patched before RTP serialization", func(t *testing.T) {
		isolateRTPHeaderFactory(t)
		harness := newDownTrackHeaderExtensionHarness(t)

		require.Equal(t, int32(1), harness.writeRTP())
		require.Equal(t, 1, harness.bwe.calls)
		require.NotZero(t, harness.bwe.lastPacketSize)

		packet, err := harness.writer.packet()
		require.NoError(t, err)
		require.Equal(t, harness.packet.ext.Packet.Payload, packet.Payload)

		var transportCC rtp.TransportCCExtension
		require.NoError(t, transportCC.Unmarshal(packet.GetExtension(harness.transportCCID)))
		require.Equal(t, harness.bwe.nextSequence, transportCC.TransportSequence)
		require.NotEqual(t, uint16(12345), transportCC.TransportSequence)
	})

	t.Run("returned headers serialize before reset and retain no extension payloads", func(t *testing.T) {
		writer := &headerExtensionWriter{}
		headerPool := &sync.Pool{}
		header := &rtp.Header{
			Version:        2,
			PayloadType:    headerExtensionPayloadType,
			SequenceNumber: 42,
			Timestamp:      9000,
			SSRC:           0x11223344,
		}
		payloads := map[uint8][]byte{
			1: {0x11, 0x12, 0x13},
			2: {0x21, 0x22, 0x23},
			3: {0x31, 0x32, 0x33},
			4: {0x41, 0x42, 0x43},
		}
		for id, payload := range payloads {
			require.NoError(t, header.SetExtension(id, payload))
		}

		packet := pacer.PacketFactory.Get().(*pacer.Packet)
		*packet = pacer.Packet{
			Header:      header,
			HeaderPool:  headerPool,
			HeaderSize:  header.MarshalSize(),
			Payload:     []byte{0xa1, 0xa2, 0xa3},
			WriteStream: writer,
		}

		written, err := pacer.NewBase(logger.GetLogger(), nil).SendPacket(packet)
		require.NoError(t, err)
		require.NotZero(t, written)

		serialized, err := writer.packet()
		require.NoError(t, err)
		for id, payload := range payloads {
			require.Equal(t, payload, serialized.GetExtension(id))
		}

		returned := header
		require.Empty(t, returned.Extensions)

		// Inspect retained slots only through pion's exported Header API. A reset
		// that merely hides old descriptors with [:0] can still re-serialize the
		// old payloads when its public Extensions slice is restored to capacity.
		probe := rtp.Header{
			Version:          2,
			Extension:        true,
			ExtensionProfile: rtp.ExtensionProfileTwoByte,
			Extensions:       returned.Extensions[:cap(returned.Extensions)],
		}
		for id := range payloads {
			require.Nil(t, probe.GetExtension(id))
		}
		for _, id := range probe.GetExtensionIDs() {
			require.Zero(t, id)
		}
		expectedSize := 12 + ((4+2*len(probe.Extensions)+3)/4)*4
		require.Equal(t, expectedSize, probe.MarshalSize())
	})

	t.Run("returned headers drop oversized extension arrays", func(t *testing.T) {
		header := &rtp.Header{Version: 2, PayloadType: headerExtensionPayloadType}
		for id := uint8(1); id <= 5; id++ {
			require.NoError(t, header.SetExtension(id, []byte{id}))
		}
		require.Greater(t, cap(header.Extensions), 4)

		packet := pacer.PacketFactory.Get().(*pacer.Packet)
		*packet = pacer.Packet{
			Header:      header,
			HeaderPool:  &sync.Pool{},
			HeaderSize:  header.MarshalSize(),
			Payload:     []byte{0xa1},
			WriteStream: &headerExtensionWriter{},
		}
		_, err := pacer.NewBase(logger.GetLogger(), nil).SendPacket(packet)
		require.NoError(t, err)
		require.LessOrEqual(t, cap(header.Extensions), 4)
	})
}

func BenchmarkDownTrackWriteRTPHeaderExtensions(b *testing.B) {
	isolateRTPHeaderFactory(b)
	harness := newDownTrackHeaderExtensionHarness(b)

	// Start the forwarding lifecycle and grow the initial descriptor slice
	// outside the steady-state sample.
	if got := harness.writeRTP(); got != 1 {
		b.Fatalf("initial WriteRTP = %d, want 1", got)
	}

	for b.Loop() {
		if got := harness.writeRTP(); got != 1 {
			b.Fatalf("WriteRTP = %d, want 1", got)
		}
	}

	if harness.writer.checksum == 0 {
		b.Fatal("RTP writer did not consume serialized packet bytes")
	}
}
