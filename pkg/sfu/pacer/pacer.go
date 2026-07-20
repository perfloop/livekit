// Copyright 2023 LiveKit, Inc.
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
	"sync"
	"time"

	"github.com/livekit/livekit-server/pkg/sfu/ccutils"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

const retainedHeaderExtensionCapacity = 2

var (
	PacketFactory = &sync.Pool{
		New: func() any {
			return &Packet{}
		},
	}

	retainedHeaderPool = &sync.Pool{
		New: func() any {
			return &retainedHeader{}
		},
	}
)

// --------------------------------------

type PacerBehavior string

const (
	PacerBehaviorPassThrough PacerBehavior = "pass-through"
	PacerBehaviorNoQueue     PacerBehavior = "no-queue"
	PacerBehaviorLeakybucket PacerBehavior = "leaky-bucket"
)

type retainedHeader struct {
	header     rtp.Header
	extensions [retainedHeaderExtensionCapacity]rtp.Extension
}

// GetPacketWithRetainedHeader returns a packet whose header owns a fixed two-extension backing array.
func GetPacketWithRetainedHeader() *Packet {
	packet := PacketFactory.Get().(*Packet)
	header := retainedHeaderPool.Get().(*retainedHeader)
	header.header = rtp.Header{Extensions: header.extensions[:0]}
	*packet = Packet{
		Header:         &header.header,
		retainedHeader: header,
	}
	return packet
}

type Packet struct {
	Header             *rtp.Header
	HeaderPool         *sync.Pool
	HeaderSize         int
	Payload            []byte
	IsRTX              bool
	ProbeClusterId     ccutils.ProbeClusterId
	IsProbe            bool
	AbsSendTimeExtID   uint8
	TransportWideExtID uint8
	WriteStream        webrtc.TrackLocalWriter
	Pool               *sync.Pool
	PoolEntity         *[]byte

	retainedHeader *retainedHeader
}

type Pacer interface {
	Enqueue(p *Packet)
	Stop()

	SetInterval(interval time.Duration)
	SetBitrate(bitrate int)

	TimeSinceLastSentPacket() time.Duration

	SetPacerProbeObserverListener(listener PacerProbeObserverListener)
	StartProbeCluster(pci ccutils.ProbeClusterInfo)
	EndProbeCluster(probeClusterId ccutils.ProbeClusterId) ccutils.ProbeClusterInfo
}

type PacerProbeObserverListener interface {
	OnPacerProbeObserverClusterComplete(probeClusterId ccutils.ProbeClusterId)
}

// ------------------------------------------------
