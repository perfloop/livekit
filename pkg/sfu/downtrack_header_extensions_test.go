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
	"io"
	"sync"
	"testing"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/livekit-server/pkg/sfu/bwe"
	"github.com/livekit/livekit-server/pkg/sfu/ccutils"
	"github.com/livekit/livekit-server/pkg/sfu/pacer"
	"github.com/livekit/livekit-server/pkg/sfu/rtpstats"
)

const (
	headerExtensionsAbsSendTimeID uint8 = 3
	headerExtensionsTransportCCID uint8 = 5
)

type headerExtensionsBWE struct {
	bwe.NullBWE

	calls        int
	lastSize     int
	nextSequence uint16
}

func (b *headerExtensionsBWE) Type() bwe.BWEType {
	return bwe.BWETypeSendSide
}

func (b *headerExtensionsBWE) RecordPacketSendAndGetSequenceNumber(
	_atMicro int64,
	size int,
	_isRTX bool,
	_probeClusterID ccutils.ProbeClusterId,
	_isProbe bool,
) uint16 {
	b.calls++
	b.lastSize = size
	b.nextSequence++
	return b.nextSequence
}

type headerExtensionsWriter struct {
	packet [1500]byte
	n      int
	writes int
}

func (w *headerExtensionsWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	headerSize := header.MarshalSize()
	if headerSize+len(payload) > len(w.packet) {
		return 0, io.ErrShortBuffer
	}

	n, err := header.MarshalTo(w.packet[:headerSize])
	if err != nil {
		return 0, err
	}
	n += copy(w.packet[n:], payload)
	w.n = n
	w.writes++
	return n, nil
}

func (w *headerExtensionsWriter) Write(packet []byte) (int, error) {
	if len(packet) > len(w.packet) {
		return 0, io.ErrShortBuffer
	}

	w.n = copy(w.packet[:], packet)
	w.writes++
	return w.n, nil
}

type headerExtensionsDownTrackListener struct{}

func (headerExtensionsDownTrackListener) OnBindAndConnected() {}

func (headerExtensionsDownTrackListener) OnStatsUpdate(_ *livekit.AnalyticsStat) {}

func (headerExtensionsDownTrackListener) OnMaxSubscribedLayerChanged(_ int32) {}

func (headerExtensionsDownTrackListener) OnRttUpdate(_ uint32) {}

func (headerExtensionsDownTrackListener) OnCodecNegotiated(_ webrtc.RTPCodecCapability) {}

func (headerExtensionsDownTrackListener) OnDownTrackClose(_ bool) {}

func (headerExtensionsDownTrackListener) OnStreamStarted() {}

type headerExtensionsWorkload struct {
	downTrack *DownTrack
	extPacket *buffer.ExtPacket
	writer    *headerExtensionsWriter
	bwe       *headerExtensionsBWE
}

func newHeaderExtensionsWorkload() *headerExtensionsWorkload {
	stats := rtpstats.NewRTPStatsSender(rtpstats.RTPStatsParams{}, 16)
	stats.SetClockRate(48_000)

	writer := &headerExtensionsWriter{}
	estimator := &headerExtensionsBWE{nextSequence: 10_000}
	forwarder := NewForwarder(
		webrtc.RTPCodecTypeAudio,
		logger.GetLogger(),
		true, // skipReferenceTS
		false,
		false,
		stats,
	)
	downTrack := &DownTrack{
		params: DownTrackParams{
			Logger:   logger.GetLogger(),
			Listener: headerExtensionsDownTrackListener{},
		},
		kind:               webrtc.RTPCodecTypeAudio,
		ssrc:               0x10203040,
		forwarder:          forwarder,
		rtpStats:           stats,
		absSendTimeExtID:   int(headerExtensionsAbsSendTimeID),
		transportWideExtID: int(headerExtensionsTransportCCID),
		writeStream:        writer,
		pacer:              pacer.NewPassThrough(logger.GetLogger(), estimator),
	}
	downTrack.payloadType.Store(111)
	downTrack.writable.Store(true)

	packet := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			Marker:         true,
			PayloadType:    111,
			SequenceNumber: 100,
			Timestamp:      960,
			SSRC:           0x55667788,
		},
		Payload: []byte{0xf8, 0xff, 0xfe, 0x01},
	}

	return &headerExtensionsWorkload{
		downTrack: downTrack,
		extPacket: &buffer.ExtPacket{
			Arrival:           1,
			ExtSequenceNumber: uint64(packet.SequenceNumber),
			ExtTimestamp:      uint64(packet.Timestamp),
			Packet:            packet,
		},
		writer: writer,
		bwe:    estimator,
	}
}

func (w *headerExtensionsWorkload) writePacket() int32 {
	w.extPacket.Packet.SequenceNumber++
	w.extPacket.Packet.Timestamp += 960
	w.extPacket.ExtSequenceNumber++
	w.extPacket.ExtTimestamp += 960
	w.extPacket.Arrival++
	return w.downTrack.WriteRTP(w.extPacket, 0)
}

func unmarshalHeaderExtensionsPacket(t testing.TB, writer *headerExtensionsWriter) *rtp.Packet {
	t.Helper()

	packet := &rtp.Packet{}
	if err := packet.Unmarshal(writer.packet[:writer.n]); err != nil {
		t.Fatalf("unmarshal written RTP packet: %v", err)
	}
	return packet
}

func TestDownTrackWriteRTPPatchesHeaderExtensions(t *testing.T) {
	workload := newHeaderExtensionsWorkload()

	if sent := workload.writePacket(); sent != 1 {
		t.Fatalf("WriteRTP sent %d packets, want 1", sent)
	}
	if workload.writer.writes != 1 {
		t.Fatalf("writer calls = %d, want 1", workload.writer.writes)
	}
	if workload.bwe.calls != 1 {
		t.Fatalf("BWE calls = %d, want 1", workload.bwe.calls)
	}
	if workload.bwe.lastSize != workload.writer.n {
		t.Fatalf("BWE packet size = %d, written RTP size = %d", workload.bwe.lastSize, workload.writer.n)
	}

	packet := unmarshalHeaderExtensionsPacket(t, workload.writer)
	if got := packet.GetExtension(headerExtensionsAbsSendTimeID); len(got) != 3 {
		t.Fatalf("abs-send-time payload length = %d, want 3", len(got))
	}

	transportCC := &rtp.TransportCCExtension{}
	if err := transportCC.Unmarshal(packet.GetExtension(headerExtensionsTransportCCID)); err != nil {
		t.Fatalf("unmarshal transport-cc extension: %v", err)
	}
	if transportCC.TransportSequence != workload.bwe.nextSequence {
		t.Fatalf("transport-cc sequence = %d, want BWE sequence %d", transportCC.TransportSequence, workload.bwe.nextSequence)
	}
}

func TestBaseSendPacketClearsRetainedHeaderExtensions(t *testing.T) {
	header := &rtp.Header{
		Version:        2,
		PayloadType:    111,
		SequenceNumber: 77,
		Timestamp:      9_600,
		SSRC:           0x10203040,
	}
	for id, payload := range map[uint8][]byte{
		1: {0x01, 0x02, 0x03},
		2: {0x04, 0x05},
	} {
		if err := header.SetExtension(id, payload); err != nil {
			t.Fatalf("set extension %d: %v", id, err)
		}
	}

	writer := &headerExtensionsWriter{}
	packet := &pacer.Packet{
		Header:      header,
		HeaderPool:  &sync.Pool{},
		HeaderSize:  header.MarshalSize(),
		Payload:     []byte{0xf8, 0xff, 0xfe, 0x01},
		WriteStream: writer,
	}
	if _, err := pacer.NewBase(logger.GetLogger(), nil).SendPacket(packet); err != nil {
		t.Fatalf("send packet: %v", err)
	}

	written := unmarshalHeaderExtensionsPacket(t, writer)
	if got := written.GetExtension(1); !bytes.Equal(got, []byte{0x01, 0x02, 0x03}) {
		t.Fatalf("serialized extension 1 = %x", got)
	}
	if got := written.GetExtension(2); !bytes.Equal(got, []byte{0x04, 0x05}) {
		t.Fatalf("serialized extension 2 = %x", got)
	}
	if len(header.Extensions) != 0 {
		t.Fatalf("returned header has %d active extensions", len(header.Extensions))
	}

	// The baseline discards the descriptor backing array. An optimized reset may
	// retain it, but no prior payload may remain observable through rtp.Header's
	// public extension API when that retained capacity is resliced.
	if cap(header.Extensions) == 0 {
		return
	}
	retained := rtp.Header{
		Extension:  true,
		Extensions: header.Extensions[:cap(header.Extensions)],
	}
	for id := 0; id <= 255; id++ {
		if payload := retained.GetExtension(uint8(id)); payload != nil {
			t.Fatalf("retained extension %d exposes payload %x after reset", id, payload)
		}
	}
}

func BenchmarkDownTrackWriteRTPHeaderExtensions(b *testing.B) {
	workload := newHeaderExtensionsWorkload()
	if sent := workload.writePacket(); sent != 1 {
		b.Fatalf("warmup WriteRTP sent %d packets, want 1", sent)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if sent := workload.writePacket(); sent != 1 {
			b.Fatalf("WriteRTP sent %d packets, want 1", sent)
		}
	}
	if workload.writer.writes == 0 {
		b.Fatal("writer did not consume a packet")
	}
}
