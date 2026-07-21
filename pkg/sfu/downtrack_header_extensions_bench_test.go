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
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/require"

	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/livekit-server/pkg/sfu/bwe"
	"github.com/livekit/livekit-server/pkg/sfu/ccutils"
	"github.com/livekit/livekit-server/pkg/sfu/pacer"
	"github.com/livekit/livekit-server/pkg/sfu/rtpstats"
	"github.com/livekit/livekit-server/pkg/sfu/testutils"
)

const (
	headerExtensionsAbsSendTimeID   = 3
	headerExtensionsTransportWideID = 5
)

type headerExtensionsBWE struct {
	bwe.NullBWE

	nextSequence uint16
	lastSequence uint16
	calls        int
	atMicro      int64
	packetSize   int
}

func (b *headerExtensionsBWE) Type() bwe.BWEType {
	return bwe.BWETypeSendSide
}

func (b *headerExtensionsBWE) RecordPacketSendAndGetSequenceNumber(
	atMicro int64,
	packetSize int,
	_ bool,
	_ ccutils.ProbeClusterId,
	_ bool,
) uint16 {
	b.calls++
	b.atMicro = atMicro
	b.packetSize = packetSize
	b.lastSequence = b.nextSequence
	b.nextSequence++
	return b.lastSequence
}

type headerExtensionsCapturingWriter struct {
	header  []byte
	payload []byte
}

func (w *headerExtensionsCapturingWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	marshaled, err := header.Marshal()
	if err != nil {
		return 0, err
	}

	w.header = append(w.header[:0], marshaled...)
	w.payload = append(w.payload[:0], payload...)
	return len(marshaled) + len(payload), nil
}

func (w *headerExtensionsCapturingWriter) Write(packet []byte) (int, error) {
	w.payload = append(w.payload[:0], packet...)
	return len(packet), nil
}

type headerExtensionsBenchmarkWriter struct {
	buffer   []byte
	checksum uint64
}

func (w *headerExtensionsBenchmarkWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	written, err := header.MarshalTo(w.buffer[:header.MarshalSize()])
	if err != nil {
		return 0, err
	}

	w.checksum += uint64(w.buffer[written-1]) + uint64(payload[0]) + uint64(written)
	return written + len(payload), nil
}

func (w *headerExtensionsBenchmarkWriter) Write(packet []byte) (int, error) {
	w.checksum += uint64(len(packet))
	return len(packet), nil
}

func newHeaderExtensionsPacket(sequence uint64) *buffer.ExtPacket {
	extPkt, err := testutils.GetTestExtPacket(&testutils.TestExtPacketParams{
		PayloadType:    111,
		SequenceNumber: uint16(sequence),
		Timestamp:      uint32(sequence * 960),
		SSRC:           0x12345678,
		PayloadSize:    120,
		ArrivalTime:    time.Unix(0, int64(sequence)*int64(20*time.Millisecond)),
	})
	if err != nil {
		panic(err)
	}

	extPkt.ExtSequenceNumber = sequence
	extPkt.ExtTimestamp = sequence * 960
	return extPkt
}

func setHeaderExtensionsPacketSequence(extPkt *buffer.ExtPacket, sequence uint64) {
	extPkt.Packet.SequenceNumber = uint16(sequence)
	extPkt.Packet.Timestamp = uint32(sequence * 960)
	extPkt.ExtSequenceNumber = sequence
	extPkt.ExtTimestamp = sequence * 960
	extPkt.Arrival = int64(sequence) * int64(20*time.Millisecond)
}

func newHeaderExtensionsDownTrack(writer webrtc.TrackLocalWriter, sender bwe.BWE) *DownTrack {
	stats := rtpstats.NewRTPStatsSender(rtpstats.RTPStatsParams{}, 128)
	stats.SetClockRate(testutils.TestOpusCodec.ClockRate)

	forwarder := NewForwarder(
		webrtc.RTPCodecTypeAudio,
		logger.GetLogger(),
		true,  // skipReferenceTS
		true,  // disableOpportunisticAllocation
		false, // enableStartAtDesiredQuality
		stats,
	)
	forwarder.DetermineCodec(testutils.TestOpusCodec, nil, livekit.VideoLayer_MODE_UNUSED)

	// Start from a forwarding state so the focused workload stays on WriteRTP's
	// per-packet path rather than measuring stream-start callbacks.
	prime := newHeaderExtensionsPacket(1)
	forwarder.started = true
	forwarder.lastSSRC = prime.Packet.SSRC
	forwarder.lastReferencePayloadType = int8(prime.Packet.PayloadType)
	forwarder.rtpMunger.SetLastSnTs(prime)

	downTrack := &DownTrack{
		params: DownTrackParams{
			Logger: logger.GetLogger(),
		},
		kind:               webrtc.RTPCodecTypeAudio,
		ssrc:               0x87654321,
		forwarder:          forwarder,
		rtpStats:           stats,
		pacer:              pacer.NewPassThrough(logger.GetLogger(), sender),
		writeStream:        writer,
		absSendTimeExtID:   headerExtensionsAbsSendTimeID,
		transportWideExtID: headerExtensionsTransportWideID,
	}
	downTrack.payloadType.Store(111)
	downTrack.writable.Store(true)
	return downTrack
}

func TestDownTrackWriteRTPHeaderExtensions(t *testing.T) {
	writer := &headerExtensionsCapturingWriter{}
	sender := &headerExtensionsBWE{nextSequence: 0xd00d}
	downTrack := newHeaderExtensionsDownTrack(writer, sender)

	require.Equal(t, int32(1), downTrack.WriteRTP(newHeaderExtensionsPacket(2), 0))
	require.Equal(t, 1, sender.calls)
	require.NotZero(t, sender.atMicro)
	require.NotEmpty(t, writer.header)

	var sentHeader rtp.Header
	_, err := sentHeader.Unmarshal(writer.header)
	require.NoError(t, err)

	absSendTime := sentHeader.GetExtension(headerExtensionsAbsSendTimeID)
	require.Len(t, absSendTime, rtp.AbsSendTimeExtension{}.MarshalSize())
	var absSendTimeExtension rtp.AbsSendTimeExtension
	require.NoError(t, absSendTimeExtension.Unmarshal(absSendTime))
	require.NotZero(t, absSendTimeExtension.Timestamp)

	transportCC := sentHeader.GetExtension(headerExtensionsTransportWideID)
	var transportCCExtension rtp.TransportCCExtension
	require.NoError(t, transportCCExtension.Unmarshal(transportCC))
	require.Equal(t, uint16(0xd00d), transportCCExtension.TransportSequence)
	require.Equal(t, sender.lastSequence, transportCCExtension.TransportSequence)
	require.NotEqual(t, uint16(12345), transportCCExtension.TransportSequence)
	require.Equal(t, len(writer.header)+len(writer.payload), sender.packetSize)
}

func TestBaseSendPacketClearsRetainedExtensionPayloads(t *testing.T) {
	writer := &headerExtensionsCapturingWriter{}
	base := pacer.NewBase(logger.GetLogger(), &headerExtensionsBWE{})
	headerPool := &sync.Pool{}
	header := &rtp.Header{
		Version:        2,
		PayloadType:    111,
		SequenceNumber: 1,
		Timestamp:      960,
		SSRC:           0x87654321,
	}

	oldExtensions := []struct {
		id      uint8
		payload []byte
	}{
		{id: 1, payload: []byte{0xd1, 0xd1, 0xd1, 0xd1}},
		{id: 2, payload: []byte{0xd2, 0xd2, 0xd2, 0xd2}},
		{id: 3, payload: []byte{0xd3, 0xd3, 0xd3, 0xd3}},
		{id: 4, payload: []byte{0xd4, 0xd4, 0xd4, 0xd4}},
		{id: 5, payload: []byte{0xd5, 0xd5, 0xd5, 0xd5}},
	}
	for _, extension := range oldExtensions {
		require.NoError(t, header.SetExtension(extension.id, extension.payload))
	}

	_, err := base.SendPacket(&pacer.Packet{
		Header:      header,
		HeaderPool:  headerPool,
		Payload:     []byte{0xaa},
		WriteStream: writer,
	})
	require.NoError(t, err)

	var serialized rtp.Header
	_, err = serialized.Unmarshal(writer.header)
	require.NoError(t, err)
	for _, extension := range oldExtensions {
		require.Equal(t, extension.payload, serialized.GetExtension(extension.id))
	}

	// Reuse the same retained backing storage for a smaller packet. This models
	// the capacity-preserving WriteRTP initialization while leaving old slots
	// beyond the visible length for SendPacket to clear.
	retainedExtensions := header.Extensions[:0]
	*header = rtp.Header{
		Version:        2,
		PayloadType:    111,
		SequenceNumber: 2,
		Timestamp:      1920,
		SSRC:           0x87654321,
		Extensions:     retainedExtensions,
	}
	for _, extension := range []struct {
		id      uint8
		payload []byte
	}{
		{id: 1, payload: []byte{0xa1}},
		{id: 2, payload: []byte{0xa2, 0xa2}},
	} {
		require.NoError(t, header.SetExtension(extension.id, extension.payload))
	}

	_, err = base.SendPacket(&pacer.Packet{
		Header:      header,
		HeaderPool:  headerPool,
		Payload:     []byte{0xbb},
		WriteStream: writer,
	})
	require.NoError(t, err)
	require.False(t, header.Extension)
	require.Empty(t, header.Extensions)

	// Header.Extensions, Header.GetExtension, and Header.Marshal are public Pion
	// APIs. Reopening each retained slot makes an old payload observable either
	// through its original ID or, if an incomplete reset cleared only IDs,
	// through RTP serialization. This exercises slots beyond the visible two.
	retainedSlots := header.Extensions[:cap(header.Extensions)]
	header.Extensions = retainedSlots
	header.Extension = true
	for _, extension := range oldExtensions {
		require.Nil(t, header.GetExtension(extension.id), "extension %d retained an old payload", extension.id)
	}
	for index := range retainedSlots {
		header.Extensions = retainedSlots[index : index+1]
		header.Extension = true
		header.ExtensionProfile = rtp.ExtensionProfileOneByte
		marshaled, marshalErr := header.Marshal()
		require.NoError(t, marshalErr)
		for _, extension := range oldExtensions {
			require.False(t, bytes.Contains(marshaled, extension.payload), "retained slot %d exposed an old payload", index)
		}
	}
}

func BenchmarkDownTrackWriteRTPHeaderExtensions(b *testing.B) {
	writer := &headerExtensionsBenchmarkWriter{buffer: make([]byte, 128)}
	sender := &headerExtensionsBWE{nextSequence: 1}
	downTrack := newHeaderExtensionsDownTrack(writer, sender)
	extPkt := newHeaderExtensionsPacket(2)

	// Populate all pools and advance the forwarder before timing the steady
	// state packet path.
	if downTrack.WriteRTP(extPkt, 0) != 1 {
		b.Fatal("failed to forward warm-up packet")
	}

	sender.calls = 0
	writer.checksum = 0
	b.ResetTimer()
	for sequence := uint64(3); b.Loop(); sequence++ {
		setHeaderExtensionsPacketSequence(extPkt, sequence)
		if downTrack.WriteRTP(extPkt, 0) != 1 {
			b.Fatal("failed to forward packet")
		}
	}

	if sender.calls == 0 || writer.checksum == 0 {
		b.Fatal("benchmark did not consume forwarded packets")
	}
}
