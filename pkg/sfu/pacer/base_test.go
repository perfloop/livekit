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

package pacer

import (
	"testing"

	"github.com/livekit/livekit-server/pkg/sfu/bwe"
	"github.com/livekit/livekit-server/pkg/sfu/ccutils"
	"github.com/livekit/protocol/logger"
	"github.com/pion/rtp"
	"github.com/stretchr/testify/require"
)

type patchRTPHeaderExtensionsTestBWE struct {
	bwe.NullBWE

	sequence uint16
}

func (*patchRTPHeaderExtensionsTestBWE) Type() bwe.BWEType {
	return bwe.BWETypeNone
}

func (b *patchRTPHeaderExtensionsTestBWE) RecordPacketSendAndGetSequenceNumber(
	_ int64,
	_ int,
	_ bool,
	_ ccutils.ProbeClusterId,
	_ bool,
) uint16 {
	b.sequence++
	return b.sequence
}

func newPatchRTPHeaderExtensionsBase() (*Base, *patchRTPHeaderExtensionsTestBWE) {
	transportBWE := &patchRTPHeaderExtensionsTestBWE{}
	return NewBase(logger.GetLogger(), transportBWE), transportBWE
}

func newPatchRTPHeaderExtensionsPacket() *Packet {
	header := &rtp.Header{
		Version:        2,
		PayloadType:    96,
		SequenceNumber: 1234,
		Timestamp:      5678,
		SSRC:           9012,
	}
	if err := header.SetExtension(1, []byte{0, 0, 0}); err != nil {
		panic(err)
	}
	if err := header.SetExtension(2, []byte{0, 0}); err != nil {
		panic(err)
	}

	return &Packet{
		Header:             header,
		HeaderSize:         header.MarshalSize(),
		Payload:            make([]byte, 1200),
		AbsSendTimeExtID:   1,
		TransportWideExtID: 2,
	}
}

func requirePatchedRTPHeaderExtensions(t testing.TB, packet *Packet, wantTransportSequence uint16) {
	t.Helper()
	require.Equal(t, packet.HeaderSize, packet.Header.MarshalSize())

	rawHeader, err := packet.Header.Marshal()
	require.NoError(t, err)

	var marshaledHeader rtp.Header
	_, err = marshaledHeader.Unmarshal(rawHeader)
	require.NoError(t, err)

	var absSendTime rtp.AbsSendTimeExtension
	require.NoError(t, absSendTime.Unmarshal(marshaledHeader.GetExtension(packet.AbsSendTimeExtID)))
	require.NotZero(t, absSendTime.Timestamp)

	var transportCC rtp.TransportCCExtension
	require.NoError(t, transportCC.Unmarshal(marshaledHeader.GetExtension(packet.TransportWideExtID)))
	require.Equal(t, wantTransportSequence, transportCC.TransportSequence)
}

func TestBasePatchRTPHeaderExtensions(t *testing.T) {
	base, transportBWE := newPatchRTPHeaderExtensionsBase()
	firstPacket := newPatchRTPHeaderExtensionsPacket()
	require.NoError(t, base.patchRTPHeaderExtensions(firstPacket))
	requirePatchedRTPHeaderExtensions(t, firstPacket, transportBWE.sequence)

	firstAbsSendTime := append([]byte(nil), firstPacket.Header.GetExtension(firstPacket.AbsSendTimeExtID)...)
	firstTransportCC := append([]byte(nil), firstPacket.Header.GetExtension(firstPacket.TransportWideExtID)...)

	secondPacket := newPatchRTPHeaderExtensionsPacket()
	require.NoError(t, base.patchRTPHeaderExtensions(secondPacket))
	requirePatchedRTPHeaderExtensions(t, secondPacket, transportBWE.sequence)

	require.Equal(t, firstAbsSendTime, firstPacket.Header.GetExtension(firstPacket.AbsSendTimeExtID))
	require.Equal(t, firstTransportCC, firstPacket.Header.GetExtension(firstPacket.TransportWideExtID))
	requirePatchedRTPHeaderExtensions(t, firstPacket, 1)
}

func TestBasePatchRTPHeaderExtensionsAllocationControl(t *testing.T) {
	base, transportBWE := newPatchRTPHeaderExtensionsBase()
	packet := newPatchRTPHeaderExtensionsPacket()

	require.NoError(t, base.patchRTPHeaderExtensions(packet))
	requirePatchedRTPHeaderExtensions(t, packet, transportBWE.sequence)
	firstTransportCC := append([]byte(nil), packet.Header.GetExtension(packet.TransportWideExtID)...)

	// Header retains extension payload slices, so a repeated patch must replace
	// the previously serialized transport sequence on the same Packet.
	require.NoError(t, base.patchRTPHeaderExtensions(packet))
	requirePatchedRTPHeaderExtensions(t, packet, transportBWE.sequence)
	require.NotEqual(t, firstTransportCC, packet.Header.GetExtension(packet.TransportWideExtID))
	t.Logf("extension-allocation-control enabled=%d", transportBWE.sequence)
}

func BenchmarkBasePatchRTPHeaderExtensions(b *testing.B) {
	base, transportBWE := newPatchRTPHeaderExtensionsBase()
	packet := newPatchRTPHeaderExtensionsPacket()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := base.patchRTPHeaderExtensions(packet); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	requirePatchedRTPHeaderExtensions(b, packet, transportBWE.sequence)
}
