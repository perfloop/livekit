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
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/require"

	"github.com/livekit/mediatransportutil/pkg/codec"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/livekit-server/pkg/sfu/ccutils"
	"github.com/livekit/livekit-server/pkg/sfu/pacer"
	"github.com/livekit/livekit-server/pkg/sfu/rtpstats"
	"github.com/livekit/livekit-server/pkg/sfu/testutils"
)

const (
	forwarderVP8ForwardingPayloadSize = 1200
	forwarderVP8ForwardingPacketCount = 0x7e00
)

type noopVP8DownTrackListener struct{}

func (noopVP8DownTrackListener) OnBindAndConnected() {}

func (noopVP8DownTrackListener) OnStatsUpdate(*livekit.AnalyticsStat) {}

func (noopVP8DownTrackListener) OnMaxSubscribedLayerChanged(int32) {}

func (noopVP8DownTrackListener) OnRttUpdate(uint32) {}

func (noopVP8DownTrackListener) OnCodecNegotiated(webrtc.RTPCodecCapability) {}

func (noopVP8DownTrackListener) OnDownTrackClose(bool) {}

func (noopVP8DownTrackListener) OnStreamStarted() {}

type vp8ForwardingPacer struct {
	outboundPayload []byte
	expectedHeader  []byte
	expectedMedia   []byte
	valid           bool
	packets         int
}

var _ pacer.Pacer = (*vp8ForwardingPacer)(nil)

func (p *vp8ForwardingPacer) Enqueue(packet *pacer.Packet) {
	n := copy(p.outboundPayload, packet.Payload)
	p.valid = n == len(packet.Payload) &&
		len(packet.Payload) == len(p.expectedHeader)+len(p.expectedMedia) &&
		bytes.Equal(p.outboundPayload[:len(p.expectedHeader)], p.expectedHeader) &&
		bytes.Equal(p.outboundPayload[len(p.expectedHeader):n], p.expectedMedia)
	p.packets++

	if packet.HeaderPool != nil && packet.Header != nil {
		*packet.Header = rtp.Header{}
		packet.HeaderPool.Put(packet.Header)
	}
	if packet.Pool != nil && packet.PoolEntity != nil {
		packet.Pool.Put(packet.PoolEntity)
	}
	*packet = pacer.Packet{}
	pacer.PacketFactory.Put(packet)
}

func (*vp8ForwardingPacer) Stop() {}

func (*vp8ForwardingPacer) SetInterval(time.Duration) {}

func (*vp8ForwardingPacer) SetBitrate(int) {}

func (*vp8ForwardingPacer) TimeSinceLastSentPacket() time.Duration { return 0 }

func (*vp8ForwardingPacer) SetPacerProbeObserverListener(pacer.PacerProbeObserverListener) {}

func (*vp8ForwardingPacer) StartProbeCluster(ccutils.ProbeClusterInfo) {}

func (*vp8ForwardingPacer) EndProbeCluster(ccutils.ProbeClusterId) ccutils.ProbeClusterInfo {
	return ccutils.ProbeClusterInfoInvalid
}

func newForwarderVP8ForwardingPacket(t testing.TB) (*buffer.ExtPacket, []any, [][]byte) {
	t.Helper()

	vp8 := codec.VP8{
		FirstByte:  0x10,
		I:          true,
		M:          true,
		PictureID:  0x100,
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

	payload := make([]byte, len(inputHeader)+forwarderVP8ForwardingPayloadSize)
	copy(payload, inputHeader)
	for i := range payload[len(inputHeader):] {
		payload[len(inputHeader)+i] = byte(i)
	}

	vp8Packets := make([]any, forwarderVP8ForwardingPacketCount)
	expectedHeaders := make([][]byte, forwarderVP8ForwardingPacketCount)
	for i := range vp8Packets {
		packetVP8 := vp8
		packetVP8.PictureID = uint16(0x100 + i)
		packetVP8.TL0PICIDX = uint8(23 + i)
		packetVP8.KEYIDX = uint8(7+i) & 0x1f
		packetVP8.IsKeyFrame = i == 0
		vp8Packets[i] = packetVP8
		expectedHeaders[i], err = packetVP8.Marshal()
		require.NoError(t, err)
	}

	extPkt := &buffer.ExtPacket{
		Packet: &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				Marker:         true,
				SequenceNumber: 1_000,
				Timestamp:      90_000,
				SSRC:           0x12345678,
			},
			Payload: payload,
		},
		ExtSequenceNumber: 1_000,
		ExtTimestamp:      90_000,
		IsKeyFrame:        true,
		Payload:           vp8Packets[0],
	}
	return extPkt, vp8Packets, expectedHeaders
}

func setForwarderVP8ForwardingPacket(
	extPkt *buffer.ExtPacket,
	pacer *vp8ForwardingPacer,
	vp8Packets []any,
	expectedHeaders [][]byte,
	packetIndex uint64,
) {
	vp8PacketIndex := packetIndex % uint64(len(vp8Packets))

	extPkt.Packet.SequenceNumber = uint16(1_000 + packetIndex)
	extPkt.Packet.Timestamp = 90_000 + uint32(packetIndex)*3_000
	extPkt.ExtSequenceNumber = 1_000 + packetIndex
	extPkt.ExtTimestamp = 90_000 + packetIndex*3_000
	extPkt.IsKeyFrame = packetIndex == 0
	extPkt.Payload = vp8Packets[vp8PacketIndex]

	pacer.expectedHeader = expectedHeaders[vp8PacketIndex]
	pacer.expectedMedia = extPkt.Packet.Payload[6:]
	pacer.valid = false
}

func newForwarderVP8ForwardingDownTrack(t testing.TB) (*DownTrack, *vp8ForwardingPacer, *buffer.ExtPacket, []any, [][]byte) {
	t.Helper()

	forwarder := newForwarder(testutils.TestVP8Codec, webrtc.RTPCodecTypeVideo)
	forwarder.vls.SetTarget(buffer.VideoLayer{Spatial: 0, Temporal: 0})
	forwarder.vls.SetCurrent(buffer.InvalidLayer)

	extPkt, vp8Packets, expectedHeaders := newForwarderVP8ForwardingPacket(t)
	forwardingPacer := &vp8ForwardingPacer{
		outboundPayload: make([]byte, len(extPkt.Packet.Payload)+8),
	}

	downTrack := &DownTrack{
		params: DownTrackParams{
			Logger:   logger.GetLogger(),
			Listener: noopVP8DownTrackListener{},
		},
		kind:                webrtc.RTPCodecTypeVideo,
		ssrc:                0x87654321,
		forwarder:           forwarder,
		rtpStats:            rtpstats.NewRTPStatsSender(rtpstats.RTPStatsParams{}, 8),
		pacer:               forwardingPacer,
		maxLayerNotifierCh:  make(chan string, 1),
		keyFrameRequesterCh: make(chan struct{}, 1),
	}
	downTrack.payloadType.Store(96)
	downTrack.writable.Store(true)

	return downTrack, forwardingPacer, extPkt, vp8Packets, expectedHeaders
}

func TestDownTrackVP8ForwardedPayload(t *testing.T) {
	downTrack, forwardingPacer, extPkt, vp8Packets, expectedHeaders := newForwarderVP8ForwardingDownTrack(t)

	for packetIndex := uint64(0); packetIndex < 3; packetIndex++ {
		setForwarderVP8ForwardingPacket(extPkt, forwardingPacer, vp8Packets, expectedHeaders, packetIndex)
		require.Equal(t, int32(1), downTrack.WriteRTP(extPkt, 0))
		require.True(t, forwardingPacer.valid)
	}
}

func BenchmarkForwarderVP8TranslationForwardedPayload(b *testing.B) {
	downTrack, forwardingPacer, extPkt, vp8Packets, expectedHeaders := newForwarderVP8ForwardingDownTrack(b)

	setForwarderVP8ForwardingPacket(extPkt, forwardingPacer, vp8Packets, expectedHeaders, 0)
	if downTrack.WriteRTP(extPkt, 0) != 1 || !forwardingPacer.valid {
		b.Fatal("could not forward warmup VP8 packet into an outbound payload")
	}

	packetIndex := uint64(1)
	for b.Loop() {
		setForwarderVP8ForwardingPacket(extPkt, forwardingPacer, vp8Packets, expectedHeaders, packetIndex)
		if downTrack.WriteRTP(extPkt, 0) != 1 {
			b.Fatal("downtrack unexpectedly dropped an ordered VP8 packet")
		}
		if !forwardingPacer.valid {
			b.Fatal("downtrack did not preserve the VP8 header and media active prefix")
		}
		packetIndex++
	}
}
