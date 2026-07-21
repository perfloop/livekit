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
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/require"

	"github.com/livekit/mediatransportutil"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"

	"github.com/livekit/livekit-server/pkg/sfu/buffer"
	"github.com/livekit/livekit-server/pkg/sfu/bwe"
	"github.com/livekit/livekit-server/pkg/sfu/ccutils"
	"github.com/livekit/livekit-server/pkg/sfu/pacer"
	act "github.com/livekit/livekit-server/pkg/sfu/rtpextension/abscapturetime"
	dd "github.com/livekit/livekit-server/pkg/sfu/rtpextension/dependencydescriptor"
	pd "github.com/livekit/livekit-server/pkg/sfu/rtpextension/playoutdelay"
	"github.com/livekit/livekit-server/pkg/sfu/rtpstats"
)

const (
	headerExtensionDependencyDescriptorID uint8 = iota + 1
	headerExtensionPlayoutDelayID
	headerExtensionAbsCaptureTimeID
	headerExtensionAbsSendTimeID
	headerExtensionTransportCCID
)

var headerExtensionIDs = [...]uint8{
	headerExtensionDependencyDescriptorID,
	headerExtensionPlayoutDelayID,
	headerExtensionAbsCaptureTimeID,
	headerExtensionAbsSendTimeID,
	headerExtensionTransportCCID,
}

type headerExtensionListener struct{}

func (headerExtensionListener) OnBindAndConnected() {}

func (headerExtensionListener) OnStatsUpdate(*livekit.AnalyticsStat) {}

func (headerExtensionListener) OnMaxSubscribedLayerChanged(int32) {}

func (headerExtensionListener) OnRttUpdate(uint32) {}

func (headerExtensionListener) OnCodecNegotiated(webrtc.RTPCodecCapability) {}

func (headerExtensionListener) OnDownTrackClose(bool) {}

func (headerExtensionListener) OnStreamStarted() {}

type headerExtensionBWE struct {
	next uint16
}

func (*headerExtensionBWE) Type() bwe.BWEType { return bwe.BWETypeSendSide }

func (*headerExtensionBWE) SetBWEListener(bwe.BWEListener) {}

func (*headerExtensionBWE) Reset() {}

func (*headerExtensionBWE) HandleREMB(int64, int64, uint32, uint32) {}

func (b *headerExtensionBWE) RecordPacketSendAndGetSequenceNumber(
	_ int64,
	_ int,
	_ bool,
	_ ccutils.ProbeClusterId,
	_ bool,
) uint16 {
	b.next++
	return b.next
}

func (*headerExtensionBWE) HandleTWCCFeedback(*rtcp.TransportLayerCC) {}

func (*headerExtensionBWE) UpdateRTT(float64) {}

func (*headerExtensionBWE) CongestionState() bwe.CongestionState { return bwe.CongestionStateNone }

func (*headerExtensionBWE) CanProbe() bool { return false }

func (*headerExtensionBWE) ProbeDuration() time.Duration { return 0 }

func (*headerExtensionBWE) ProbeClusterStarting(ccutils.ProbeClusterInfo) {}

func (*headerExtensionBWE) ProbeClusterDone(ccutils.ProbeClusterInfo) {}

func (*headerExtensionBWE) ProbeClusterIsGoalReached() bool { return false }

func (*headerExtensionBWE) ProbeClusterFinalize() (ccutils.ProbeSignal, int64, bool) {
	return ccutils.ProbeSignalInconclusive, 0, false
}

type headerExtensionWriter struct {
	header   rtp.Header
	payload  []byte
	checksum uint64
	writes   int
	record   bool
}

func (w *headerExtensionWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	w.writes++
	w.checksum += uint64(header.SequenceNumber) + uint64(header.Timestamp) + uint64(len(payload))
	for _, id := range headerExtensionIDs {
		extension := header.GetExtension(id)
		w.checksum += uint64(id) + uint64(len(extension))
		if len(extension) != 0 {
			w.checksum += uint64(extension[0])
		}
	}

	if w.record {
		marshaled, err := header.Marshal()
		if err != nil {
			return 0, err
		}
		var copied rtp.Header
		if _, err = copied.Unmarshal(marshaled); err != nil {
			return 0, err
		}
		w.header = copied
		w.payload = append(w.payload[:0], payload...)
	}

	return header.MarshalSize() + len(payload), nil
}

func (w *headerExtensionWriter) Write(payload []byte) (int, error) {
	w.checksum += uint64(len(payload))
	return len(payload), nil
}

type headerExtensionFixture struct {
	downtrack *DownTrack
	packet    *buffer.ExtPacket
	frame     uint64
	bwe       *headerExtensionBWE
	actBytes  []byte
}

func newHeaderExtensionFixture(writer webrtc.TrackLocalWriter) *headerExtensionFixture {
	lgr := logger.GetLogger()
	stats := rtpstats.NewRTPStatsSender(rtpstats.RTPStatsParams{}, 128)
	stats.SetClockRate(90000)

	forwarder := NewForwarder(
		webrtc.RTPCodecTypeVideo,
		lgr,
		true, // skipReferenceTS
		true, // disableOpportunisticAllocation
		true, // enableStartAtDesiredQuality
		stats,
	)
	forwarder.DetermineCodec(
		webrtc.RTPCodecCapability{MimeType: "video/AV1", ClockRate: 90000},
		[]webrtc.RTPHeaderExtensionParameter{{
			ID:  int(headerExtensionDependencyDescriptorID),
			URI: dd.ExtensionURI,
		}},
		livekit.VideoLayer_MODE_UNUSED,
	)
	forwarder.vls.SetTarget(buffer.VideoLayer{Spatial: 0, Temporal: 0})

	playoutDelay, err := NewPlayoutDelayController(100, 120, lgr, stats)
	if err != nil {
		panic(err)
	}

	bwe := &headerExtensionBWE{}
	fixture := &headerExtensionFixture{
		bwe:    bwe,
		packet: newHeaderExtensionPacket(1, true),
		frame:  1,
	}
	fixture.packet.AbsCaptureTimeExt = act.AbsCaptureTimeFromValue(
		uint64(mediatransportutil.ToNtpTime(time.Now())),
		0,
	)

	fixture.downtrack = &DownTrack{
		params: DownTrackParams{
			Logger:   lgr,
			Listener: headerExtensionListener{},
		},
		kind:                      webrtc.RTPCodecTypeVideo,
		ssrc:                      0x10203040,
		forwarder:                 forwarder,
		dependencyDescriptorExtID: int(headerExtensionDependencyDescriptorID),
		playoutDelayExtID:         int(headerExtensionPlayoutDelayID),
		absCaptureTimeExtID:       int(headerExtensionAbsCaptureTimeID),
		absSendTimeExtID:          int(headerExtensionAbsSendTimeID),
		transportWideExtID:        int(headerExtensionTransportCCID),
		playoutDelay:              playoutDelay,
		pacer:                     pacer.NewPassThrough(lgr, bwe),
		writeStream:               writer,
		rtpStats:                  stats,
	}
	fixture.downtrack.payloadType.Store(96)
	fixture.downtrack.writable.Store(true)

	if fixture.downtrack.WriteRTP(fixture.packet, 0) != 1 {
		panic("could not prime DownTrack header extension fixture")
	}
	forwarder.SetRefSenderReport(0, &livekit.RTCPSenderReportState{
		RtpTimestamp:    fixture.packet.Packet.Timestamp,
		RtpTimestampExt: fixture.packet.ExtTimestamp,
		NtpTimestamp:    uint64(mediatransportutil.ToNtpTime(time.Now())),
	})

	fixture.packet = newHeaderExtensionPacket(2, false)
	fixture.packet.AbsCaptureTimeExt = act.AbsCaptureTimeFromValue(
		uint64(mediatransportutil.ToNtpTime(time.Now())),
		0,
	)
	fixture.actBytes = mustMarshalAbsCaptureTimeValue(fixture.packet.AbsCaptureTimeExt)
	fixture.frame = 1
	if fixture.write() != 1 {
		panic("could not warm DownTrack header extension fixture")
	}

	return fixture
}

func (f *headerExtensionFixture) write() int32 {
	f.frame++
	f.packet.Packet.SequenceNumber++
	f.packet.Packet.Timestamp += 3000
	f.packet.ExtSequenceNumber++
	f.packet.ExtTimestamp += 3000
	f.packet.Arrival += int64(time.Second / 30)
	f.packet.DependencyDescriptor.Descriptor.FrameNumber = uint16(f.frame)
	f.packet.DependencyDescriptor.ExtFrameNum = f.frame
	return f.downtrack.WriteRTP(f.packet, 0)
}

func newHeaderExtensionPacket(frame uint64, keyFrame bool) *buffer.ExtPacket {
	const (
		sequenceNumber = 1234
		timestamp      = 90000
		ssrc           = 0x50607080
	)

	decodeTargets := []buffer.DependencyDescriptorDecodeTarget{{
		Target: 0,
		Layer:  buffer.VideoLayer{Spatial: 0, Temporal: 0},
	}}
	decodeTargetIndications := []dd.DecodeTargetIndication{dd.DecodeTargetSwitch}
	frameDependencies := &dd.FrameDependencyTemplate{
		SpatialId:               0,
		TemporalId:              0,
		DecodeTargetIndications: decodeTargetIndications,
	}
	descriptor := &dd.DependencyDescriptor{
		FirstPacketInFrame: true,
		LastPacketInFrame:  true,
		FrameNumber:        uint16(frame),
		FrameDependencies:  frameDependencies,
	}

	extension := &buffer.ExtDependencyDescriptor{
		Descriptor:        descriptor,
		DecodeTargets:     decodeTargets,
		Integrity:         true,
		ExtFrameNum:       frame,
		ExtKeyFrameNum:    1,
		RestartGeneration: 0,
	}
	if keyFrame {
		activeDecodeTargets := uint32(1)
		descriptor.AttachedStructure = &dd.FrameDependencyStructure{
			NumDecodeTargets: 1,
			NumChains:        0,
			Templates:        []*dd.FrameDependencyTemplate{frameDependencies},
		}
		descriptor.ActiveDecodeTargetsBitmask = &activeDecodeTargets
		extension.StructureUpdated = true
		extension.ActiveDecodeTargetsUpdated = true
	}

	packet := &rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			Marker:         true,
			PayloadType:    96,
			SequenceNumber: sequenceNumber,
			Timestamp:      timestamp,
			SSRC:           ssrc,
		},
		Payload: []byte{0x10, 0x20, 0x30, 0x40},
	}
	return &buffer.ExtPacket{
		VideoLayer:           buffer.VideoLayer{Spatial: 0, Temporal: 0},
		Arrival:              time.Now().UnixNano(),
		ExtSequenceNumber:    sequenceNumber,
		ExtTimestamp:         timestamp,
		Packet:               packet,
		DependencyDescriptor: extension,
		IsKeyFrame:           keyFrame,
	}
}

func mustMarshalAbsCaptureTimeValue(value *act.AbsCaptureTime) []byte {
	marshaled, err := value.Marshal()
	if err != nil {
		panic(err)
	}
	return marshaled
}

func TestDownTrackWriteRTPHeaderExtensions(t *testing.T) {
	writer := &headerExtensionWriter{record: true}
	fixture := newHeaderExtensionFixture(writer)

	require.Equal(t, int32(1), fixture.write())
	require.Equal(t, 3, writer.writes)
	require.Equal(t, uint8(2), writer.header.Version)
	require.Equal(t, uint8(96), writer.header.PayloadType)
	require.Equal(t, uint32(0x10203040), writer.header.SSRC)
	require.Equal(t, []byte{0x10, 0x20, 0x30, 0x40}, writer.payload)
	require.ElementsMatch(t, headerExtensionIDs[:], writer.header.GetExtensionIDs())

	require.NotEmpty(t, writer.header.GetExtension(headerExtensionDependencyDescriptorID))

	playoutDelay := &pd.PlayOutDelay{}
	require.NoError(t, playoutDelay.Unmarshal(writer.header.GetExtension(headerExtensionPlayoutDelayID)))
	require.Equal(t, uint16(100), playoutDelay.Min)
	require.Equal(t, uint16(120), playoutDelay.Max)

	require.Equal(t, fixture.actBytes, writer.header.GetExtension(headerExtensionAbsCaptureTimeID))

	absSendTime := &rtp.AbsSendTimeExtension{}
	require.NoError(t, absSendTime.Unmarshal(writer.header.GetExtension(headerExtensionAbsSendTimeID)))
	require.NotZero(t, absSendTime.Timestamp)

	transportCC := &rtp.TransportCCExtension{}
	require.NoError(t, transportCC.Unmarshal(writer.header.GetExtension(headerExtensionTransportCCID)))
	require.Equal(t, fixture.bwe.next, transportCC.TransportSequence)
}

type retainedExtensionPayload struct {
	payload [32]byte
}

type discardRTPWriter struct{}

func (discardRTPWriter) WriteRTP(header *rtp.Header, payload []byte) (int, error) {
	return header.MarshalSize() + len(payload), nil
}

func (discardRTPWriter) Write(payload []byte) (int, error) {
	return len(payload), nil
}

func TestPacerHeaderResetReleasesExtensionPayload(t *testing.T) {
	header, finalized := headerWithFinalizedExtensionPayloads(t)
	require.Empty(t, header.GetExtensionIDs())

	finalizedCount := 0
	deadline := time.Now().Add(2 * time.Second)
	for finalizedCount != len(headerExtensionIDs) && time.Now().Before(deadline) {
		runtime.GC()
		runtime.Gosched()
		for {
			select {
			case <-finalized:
				finalizedCount++
			default:
				goto drained
			}
		}
	drained:
		if finalizedCount != len(headerExtensionIDs) {
			time.Sleep(time.Millisecond)
		}
	}
	runtime.KeepAlive(header)
	require.Equal(t, len(headerExtensionIDs), finalizedCount, "pooled RTP header retained an extension payload after reset")
}

func headerWithFinalizedExtensionPayloads(t *testing.T) (*rtp.Header, <-chan struct{}) {
	t.Helper()

	owners := make([]*retainedExtensionPayload, len(headerExtensionIDs))
	finalized := make(chan struct{}, len(owners))
	header := &rtp.Header{Version: 2}
	for i, id := range headerExtensionIDs {
		owner := &retainedExtensionPayload{}
		owner.payload[0] = byte(i + 1)
		runtime.SetFinalizer(owner, func(*retainedExtensionPayload) {
			finalized <- struct{}{}
		})
		require.NoError(t, header.SetExtension(id, owner.payload[:]))
		owners[i] = owner
	}

	_, err := pacer.NewBase(logger.GetLogger(), nil).SendPacket(&pacer.Packet{
		Header:      header,
		HeaderPool:  &sync.Pool{},
		WriteStream: discardRTPWriter{},
	})
	require.NoError(t, err)
	runtime.KeepAlive(owners)
	return header, finalized
}

func BenchmarkDownTrackWriteRTPHeaderExtensions(b *testing.B) {
	writer := &headerExtensionWriter{}
	fixture := newHeaderExtensionFixture(writer)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if fixture.write() != 1 {
			b.Fatal("DownTrack did not forward RTP")
		}
	}
	b.StopTimer()
	if writer.checksum == 0 {
		b.Fatal(fmt.Sprintf("writer did not consume forwarded RTP headers after %d writes", writer.writes))
	}
}
