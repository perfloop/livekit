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
	"encoding/binary"
	"io"
	"testing"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/livekit-server/pkg/sfu/bwe"
	"github.com/livekit/livekit-server/pkg/sfu/ccutils"
	"github.com/livekit/livekit-server/pkg/sfu/pacer"
	"github.com/livekit/livekit-server/pkg/sfu/rtpstats"
	"github.com/livekit/protocol/logger"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

const (
	benchmarkAudioClockRate = 48_000
	benchmarkAudioFrameStep = 960
)

type downTrackHeaderExtensionsBenchmarkBWE struct {
	bwe.NullBWE
	sequence uint16
}

func (downTrackHeaderExtensionsBenchmarkBWE) Type() bwe.BWEType {
	return bwe.BWETypeSendSide
}

func (b *downTrackHeaderExtensionsBenchmarkBWE) RecordPacketSendAndGetSequenceNumber(
	int64,
	int,
	bool,
	ccutils.ProbeClusterId,
	bool,
) uint16 {
	b.sequence++
	return b.sequence
}

type downTrackHeaderExtensionsBenchmarkWriter struct {
	wire     [1460]byte
	wireLen  int
	writes   int
	checksum uint64
}

func (w *downTrackHeaderExtensionsBenchmarkWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	headerSize := header.MarshalSize()
	totalSize := headerSize + len(payload)
	if totalSize > len(w.wire) {
		return 0, io.ErrShortBuffer
	}

	n, err := header.MarshalTo(w.wire[:headerSize])
	if err != nil {
		return 0, err
	}
	copy(w.wire[n:totalSize], payload)
	w.wireLen = totalSize
	w.writes++
	w.checksum += uint64(w.wire[0]) + uint64(w.wire[n-1]) + uint64(w.wire[totalSize-1])
	return totalSize, nil
}

func (w *downTrackHeaderExtensionsBenchmarkWriter) Write(payload []byte) (int, error) {
	if len(payload) > len(w.wire) {
		return 0, io.ErrShortBuffer
	}
	copy(w.wire[:], payload)
	w.wireLen = len(payload)
	w.writes++
	w.checksum += uint64(w.wire[0])
	return len(payload), nil
}

func newDownTrackHeaderExtensionsBenchmark() (*DownTrack, *buffer.ExtPacket, *downTrackHeaderExtensionsBenchmarkWriter, *downTrackHeaderExtensionsBenchmarkBWE) {
	lgr := logger.GetLogger()
	stats := rtpstats.NewRTPStatsSender(rtpstats.RTPStatsParams{}, 64)
	stats.SetClockRate(benchmarkAudioClockRate)

	forwarder := NewForwarder(
		webrtc.RTPCodecTypeAudio,
		lgr,
		true,
		false,
		false,
		stats,
	)
	forwarder.DetermineCodec(
		webrtc.RTPCodecCapability{
			MimeType:  "audio/opus",
			ClockRate: benchmarkAudioClockRate,
		},
		nil,
		0,
	)

	packet := &buffer.ExtPacket{
		Arrival:           1_000_000_000,
		ExtSequenceNumber: 1_000,
		ExtTimestamp:      48_000,
		Packet: &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    111,
				SequenceNumber: 1_000,
				Timestamp:      48_000,
				SSRC:           4_321,
			},
			Payload: []byte{0xf8, 0xff, 0xfe, 0x10, 0x20, 0x30, 0x40, 0x50},
		},
	}

	forwarder.started = true
	forwarder.referenceLayerSpatial = 0
	forwarder.lastSSRC = packet.Packet.SSRC
	forwarder.lastReferencePayloadType = int8(packet.Packet.PayloadType)
	forwarder.rtpMunger.SetLastSnTs(&buffer.ExtPacket{
		ExtSequenceNumber: packet.ExtSequenceNumber - 1,
		ExtTimestamp:      packet.ExtTimestamp - benchmarkAudioFrameStep,
	})

	writer := &downTrackHeaderExtensionsBenchmarkWriter{}
	benchmarkBWE := &downTrackHeaderExtensionsBenchmarkBWE{}
	downTrack := &DownTrack{
		kind:               webrtc.RTPCodecTypeAudio,
		ssrc:               7_890,
		forwarder:          forwarder,
		rtpStats:           stats,
		pacer:              pacer.NewPassThrough(lgr, benchmarkBWE),
		writeStream:        writer,
		absSendTimeExtID:   1,
		transportWideExtID: 2,
	}
	downTrack.payloadType.Store(111)
	downTrack.writable.Store(true)

	return downTrack, packet, writer, benchmarkBWE
}

func assertDownTrackHeaderExtensionsBenchmarkOutput(t testing.TB, writer *downTrackHeaderExtensionsBenchmarkWriter, packet *buffer.ExtPacket, benchmarkBWE *downTrackHeaderExtensionsBenchmarkBWE) {
	t.Helper()

	if writer.checksum == 0 {
		t.Fatal("serialized RTP output was not consumed")
	}

	var serialized rtp.Packet
	if err := serialized.Unmarshal(writer.wire[:writer.wireLen]); err != nil {
		t.Fatal(err)
	}
	if got := serialized.GetExtension(1); len(got) != 3 {
		t.Fatalf("abs-send-time extension length = %d, want 3", len(got))
	}
	if got := serialized.GetExtension(2); len(got) != 2 {
		t.Fatalf("transport-wide extension length = %d, want 2", len(got))
	} else if got := binary.BigEndian.Uint16(got); got != benchmarkBWE.sequence {
		t.Fatalf("transport-wide extension sequence = %d, want %d", got, benchmarkBWE.sequence)
	}
	if !bytes.Equal(serialized.Payload, packet.Packet.Payload) {
		t.Fatalf("serialized payload = %x, want %x", serialized.Payload, packet.Packet.Payload)
	}
}

func TestDownTrackWriteRTPHeaderExtensions(t *testing.T) {
	downTrack, packet, writer, benchmarkBWE := newDownTrackHeaderExtensionsBenchmark()

	advanceDownTrackHeaderExtensionsBenchmarkPacket(packet)
	if got := downTrack.WriteRTP(packet, 0); got != 1 {
		t.Fatalf("first WriteRTP result = %d, want 1", got)
	}
	advanceDownTrackHeaderExtensionsBenchmarkPacket(packet)
	if got := downTrack.WriteRTP(packet, 0); got != 1 {
		t.Fatalf("second WriteRTP result = %d, want 1", got)
	}

	if writer.writes != 2 {
		t.Fatalf("writer calls = %d, want 2", writer.writes)
	}
	assertDownTrackHeaderExtensionsBenchmarkOutput(t, writer, packet, benchmarkBWE)
}

func advanceDownTrackHeaderExtensionsBenchmarkPacket(packet *buffer.ExtPacket) {
	packet.Arrival += 20_000_000
	packet.ExtSequenceNumber++
	packet.ExtTimestamp += benchmarkAudioFrameStep
	packet.Packet.SequenceNumber++
	packet.Packet.Timestamp += benchmarkAudioFrameStep
}

func BenchmarkDownTrackWriteRTPHeaderExtensions(b *testing.B) {
	downTrack, packet, writer, benchmarkBWE := newDownTrackHeaderExtensionsBenchmark()

	advanceDownTrackHeaderExtensionsBenchmarkPacket(packet)
	if got := downTrack.WriteRTP(packet, 0); got != 1 {
		b.Fatalf("warmup WriteRTP result = %d, want 1", got)
	}

	b.ReportAllocs()
	for b.Loop() {
		advanceDownTrackHeaderExtensionsBenchmarkPacket(packet)
		if got := downTrack.WriteRTP(packet, 0); got != 1 {
			b.Fatalf("WriteRTP result = %d, want 1", got)
		}
	}
	b.StopTimer()

	if writer.writes != b.N+1 {
		b.Fatalf("writer calls = %d, want %d", writer.writes, b.N+1)
	}
	assertDownTrackHeaderExtensionsBenchmarkOutput(b, writer, packet, benchmarkBWE)
}
