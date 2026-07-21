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
	"encoding/binary"
	"io"
	"reflect"
	"sync"
	"testing"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/require"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/livekit-server/pkg/sfu/bwe"
	"github.com/livekit/livekit-server/pkg/sfu/ccutils"
	"github.com/livekit/livekit-server/pkg/sfu/pacer"
	"github.com/livekit/livekit-server/pkg/sfu/rtpstats"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"
)

const (
	headerExtensionsAbsSendTimeID   = uint8(3)
	headerExtensionsTransportWideID = uint8(5)
)

type headerExtensionsBWE struct {
	bwe.NullBWE

	nextSequence uint16

	calls        int
	lastSequence uint16
	lastAtMicro  int64
	lastSize     int
}

func (b *headerExtensionsBWE) Type() bwe.BWEType {
	return bwe.BWETypeSendSide
}

func (b *headerExtensionsBWE) RecordPacketSendAndGetSequenceNumber(
	atMicro int64,
	size int,
	_isRTX bool,
	_probeClusterID ccutils.ProbeClusterId,
	_isProbe bool,
) uint16 {
	b.calls++
	b.lastAtMicro = atMicro
	b.lastSize = size
	b.lastSequence = b.nextSequence
	b.nextSequence++
	return b.lastSequence
}

type headerExtensionsWriter struct {
	buffer [1500]byte

	calls             int
	packetSize        int
	transportSequence uint16
	transportLength   int
	absSendTime       uint64
	absSendTimeLength int
	checksum          uint64
}

func (w *headerExtensionsWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	headerSize, err := header.MarshalTo(w.buffer[:])
	if err != nil {
		return 0, err
	}
	if len(payload) > len(w.buffer)-headerSize {
		return 0, io.ErrShortBuffer
	}

	copy(w.buffer[headerSize:], payload)
	w.calls++
	w.packetSize = headerSize + len(payload)
	w.checksum += uint64(w.buffer[0]) + uint64(w.buffer[w.packetSize-1]) + uint64(w.packetSize)

	transport := header.GetExtension(headerExtensionsTransportWideID)
	w.transportLength = len(transport)
	if len(transport) == 2 {
		w.transportSequence = binary.BigEndian.Uint16(transport)
	}

	absSendTime := header.GetExtension(headerExtensionsAbsSendTimeID)
	w.absSendTimeLength = len(absSendTime)
	if len(absSendTime) == 3 {
		w.absSendTime = uint64(absSendTime[0])<<16 | uint64(absSendTime[1])<<8 | uint64(absSendTime[2])
	}

	return w.packetSize, nil
}

func (w *headerExtensionsWriter) Write(payload []byte) (int, error) {
	if len(payload) > len(w.buffer) {
		return 0, io.ErrShortBuffer
	}

	copy(w.buffer[:], payload)
	w.calls++
	w.packetSize = len(payload)
	if len(payload) != 0 {
		w.checksum += uint64(w.buffer[0]) + uint64(w.buffer[len(payload)-1])
	}
	return len(payload), nil
}

type headerExtensionsListener struct{}

func (headerExtensionsListener) OnBindAndConnected() {}

func (headerExtensionsListener) OnStatsUpdate(_ *livekit.AnalyticsStat) {}

func (headerExtensionsListener) OnMaxSubscribedLayerChanged(_ int32) {}

func (headerExtensionsListener) OnRttUpdate(_ uint32) {}

func (headerExtensionsListener) OnCodecNegotiated(_ webrtc.RTPCodecCapability) {}

func (headerExtensionsListener) OnDownTrackClose(_ bool) {}

func (headerExtensionsListener) OnStreamStarted() {}

func newHeaderExtensionsDownTrack(writer webrtc.TrackLocalWriter, bandwidth bwe.BWE) *DownTrack {
	log := logger.GetLogger()
	stats := rtpstats.NewRTPStatsSender(rtpstats.RTPStatsParams{}, 64)
	stats.SetLogger(log)

	forwarder := NewForwarder(
		webrtc.RTPCodecTypeAudio,
		log,
		true,  // skipReferenceTS
		true,  // disableOpportunisticAllocation
		false, // enableStartAtDesiredQuality
		stats,
	)
	forwarder.DetermineCodec(
		webrtc.RTPCodecCapability{MimeType: "audio/opus", ClockRate: 48000},
		nil,
		livekit.VideoLayer_MODE_UNUSED,
	)

	d := &DownTrack{
		params: DownTrackParams{
			Logger:   log,
			Listener: headerExtensionsListener{},
		},
		kind:               webrtc.RTPCodecTypeAudio,
		ssrc:               0x01020304,
		forwarder:          forwarder,
		rtpStats:           stats,
		pacer:              pacer.NewPassThrough(log, bandwidth),
		writeStream:        writer,
		absSendTimeExtID:   int(headerExtensionsAbsSendTimeID),
		transportWideExtID: int(headerExtensionsTransportWideID),
	}
	d.payloadType.Store(111)
	d.writable.Store(true)

	return d
}

func newHeaderExtensionsPacket() *buffer.ExtPacket {
	packet := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			Marker:         true,
			PayloadType:    111,
			SequenceNumber: 1000,
			Timestamp:      90000,
			SSRC:           0x11223344,
		},
		Payload: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
	}

	return &buffer.ExtPacket{
		Arrival:           1,
		ExtSequenceNumber: uint64(packet.SequenceNumber),
		ExtTimestamp:      uint64(packet.Timestamp),
		Packet:            packet,
	}
}

func advanceHeaderExtensionsPacket(packet *buffer.ExtPacket) {
	packet.Packet.SequenceNumber++
	packet.Packet.Timestamp += 960
	packet.Packet.Payload[0]++
	packet.ExtSequenceNumber++
	packet.ExtTimestamp += 960
	packet.Arrival += 20_000_000
}

func TestDownTrackWriteRTPHeaderExtensions(t *testing.T) {
	writer := &headerExtensionsWriter{}
	bandwidth := &headerExtensionsBWE{nextSequence: 0xbabe}
	d := newHeaderExtensionsDownTrack(writer, bandwidth)

	written := d.WriteRTP(newHeaderExtensionsPacket(), 0)

	require.Equal(t, int32(1), written)
	require.Equal(t, 1, bandwidth.calls)
	require.NotZero(t, bandwidth.lastAtMicro)
	require.Positive(t, bandwidth.lastSize)
	require.Equal(t, bandwidth.lastSequence, writer.transportSequence)
	require.Equal(t, 2, writer.transportLength)
	require.NotEqual(t, binary.BigEndian.Uint16(dummyTransportCCExt), writer.transportSequence)
	require.Equal(t, 3, writer.absSendTimeLength)
	require.NotZero(t, writer.absSendTime)
	require.Equal(t, 1, writer.calls)
	require.Positive(t, writer.packetSize)
	require.NotZero(t, writer.checksum)
}

func TestPacerBaseClearsRetainedHeaderExtensionDescriptors(t *testing.T) {
	headerPool := &sync.Pool{}
	header := &rtp.Header{Version: 2}
	require.NoError(t, header.SetExtension(1, []byte{0xaa, 0xbb, 0xcc}))
	require.NoError(t, header.SetExtension(2, []byte{0xdd, 0xee}))
	require.Positive(t, cap(header.Extensions))

	writer := &headerExtensionsWriter{}
	base := pacer.NewBase(logger.GetLogger(), nil)
	packet := &pacer.Packet{
		Header:      header,
		HeaderPool:  headerPool,
		HeaderSize:  header.MarshalSize(),
		Payload:     []byte{0x01},
		WriteStream: writer,
	}

	_, err := base.SendPacket(packet)
	require.NoError(t, err)

	returned := headerPool.Get().(*rtp.Header)
	require.Zero(t, returned.Version)
	require.False(t, returned.Extension)
	require.Empty(t, returned.Extensions)

	// The baseline drops extension capacity. An implementation that retains it
	// must clear every descriptor, including its unexported payload reference.
	if cap(returned.Extensions) == 0 {
		return
	}

	for i, extension := range returned.Extensions[:cap(returned.Extensions)] {
		require.Truef(t, reflect.ValueOf(extension).IsZero(), "retained extension descriptor %d was not cleared", i)
	}
}

func BenchmarkDownTrackWriteRTPHeaderExtensions(b *testing.B) {
	writer := &headerExtensionsWriter{}
	bandwidth := &headerExtensionsBWE{nextSequence: 0x8000}
	d := newHeaderExtensionsDownTrack(writer, bandwidth)
	packet := newHeaderExtensionsPacket()

	// Warm the header pool before timing steady-state packet forwarding.
	if d.WriteRTP(packet, 0) != 1 {
		b.Fatal("failed to forward warm-up packet")
	}

	b.ReportAllocs()
	for b.Loop() {
		advanceHeaderExtensionsPacket(packet)
		if d.WriteRTP(packet, 0) != 1 {
			b.Fatal("failed to forward packet")
		}
	}

	if writer.calls < 2 || bandwidth.calls < 2 || writer.checksum == 0 {
		b.Fatal("forwarding workload did not reach the writer and bandwidth estimator")
	}
}
