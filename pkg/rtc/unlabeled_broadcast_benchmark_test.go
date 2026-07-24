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

package rtc

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/livekit/protocol/auth/authfakes"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/webhook"

	"github.com/livekit/livekit-server/pkg/config"
	"github.com/livekit/livekit-server/pkg/rtc/types"
	"github.com/livekit/livekit-server/pkg/rtc/types/typesfakes"
	"github.com/livekit/livekit-server/pkg/sfu"
	"github.com/livekit/livekit-server/pkg/sfu/audio"
	"github.com/livekit/livekit-server/pkg/telemetry"
	"github.com/livekit/livekit-server/pkg/telemetry/telemetryfakes"
	"github.com/livekit/livekit-server/version"
)

const (
	unlabeledBenchmarkRecipients    = 32
	unlabeledBenchmarkBurstMessages = 16
	unlabeledBenchmarkPayloadBytes  = 96
)

type recordedUnlabeledSend struct {
	data   []byte
	useRaw bool
	sender livekit.ParticipantIdentity
}

type unlabeledSendRecorder struct {
	capture bool

	calls    atomic.Uint64
	checksum atomic.Uint64

	mu    sync.Mutex
	sends []recordedUnlabeledSend
}

func (r *unlabeledSendRecorder) record(data []byte, useRaw bool, sender livekit.ParticipantIdentity) error {
	var checksum uint64
	for _, value := range data {
		checksum += uint64(value)
	}
	r.calls.Add(1)
	r.checksum.Add(checksum)

	if r.capture {
		r.mu.Lock()
		r.sends = append(r.sends, recordedUnlabeledSend{
			data:   append([]byte(nil), data...),
			useRaw: useRaw,
			sender: sender,
		})
		r.mu.Unlock()
	}

	return nil
}

func (r *unlabeledSendRecorder) reset() {
	r.calls.Store(0)
	r.checksum.Store(0)
}

func (r *unlabeledSendRecorder) snapshot() []recordedUnlabeledSend {
	r.mu.Lock()
	defer r.mu.Unlock()

	sends := make([]recordedUnlabeledSend, len(r.sends))
	for i, send := range r.sends {
		sends[i] = recordedUnlabeledSend{
			data:   append([]byte(nil), send.data...),
			useRaw: send.useRaw,
			sender: send.sender,
		}
	}
	return sends
}

func newUnlabeledBroadcastRoom(tb testing.TB, participantCount int, protocol types.ProtocolVersion) (*Room, []*typesfakes.FakeLocalParticipant) {
	tb.Helper()

	keyProvider := &authfakes.FakeKeyProvider{}
	keyProvider.GetSecretReturns("testkey")
	notifier, err := webhook.NewDefaultNotifier(webhook.DefaultWebHookConfig, keyProvider)
	if err != nil {
		tb.Fatalf("create webhook notifier: %v", err)
	}

	room := NewRoom(
		&livekit.Room{Name: "unlabeled-broadcast-benchmark"},
		nil,
		WebRTCConfig{},
		config.RoomConfig{
			EmptyTimeout:     5 * 60,
			DepartureTimeout: 1,
		},
		&sfu.AudioConfig{
			AudioLevelConfig: audio.AudioLevelConfig{
				UpdateInterval:  25,
				SmoothIntervals: 0,
			},
		},
		&livekit.ServerInfo{
			Edition:  livekit.ServerInfo_Standard,
			Version:  version.Version,
			Protocol: types.CurrentProtocol,
			NodeId:   "testnode",
			Region:   "testregion",
		},
		telemetry.NewTelemetryService(notifier, &telemetryfakes.FakeAnalyticsService{}),
		nil,
		nil,
		nil,
	)

	participants := make([]*typesfakes.FakeLocalParticipant, 0, participantCount)
	for i := 0; i < participantCount; i++ {
		participant := NewMockParticipant(
			livekit.ParticipantIdentity(fmt.Sprintf("unlabeled-%d", i)),
			protocol,
			false,
			true,
			room.LocalParticipantListener(),
		)
		if err := room.Join(participant, nil, &ParticipantOptions{AutoSubscribe: false}, nil); err != nil {
			tb.Fatalf("join participant %d: %v", i, err)
		}
		participant.StateReturns(livekit.ParticipantInfo_ACTIVE)
		participant.IsReadyReturns(true)
		participants = append(participants, participant)
	}

	return room, participants
}

func unlabeledBenchmarkPayload(sequence uint64) []byte {
	payload := make([]byte, unlabeledBenchmarkPayloadBytes)
	binary.LittleEndian.PutUint64(payload[:8], sequence)
	binary.LittleEndian.PutUint64(payload[8:16], ^sequence)
	for i := 16; i < len(payload); i++ {
		payload[i] = byte(sequence + uint64(i*31))
	}
	return payload
}

func waitForUnlabeledFanoutQuiescence(recorders []*unlabeledSendRecorder, timeout time.Duration) bool {
	const quietWindow = 2 * time.Millisecond

	deadline := time.Now().Add(timeout)
	var lastCalls uint64
	lastChange := time.Now()
	for {
		var calls uint64
		allRecipientsObserved := true
		for _, recorder := range recorders {
			count := recorder.calls.Load()
			calls += count
			if count == 0 {
				allRecipientsObserved = false
			}
		}

		now := time.Now()
		if calls != lastCalls {
			lastCalls = calls
			lastChange = now
		}
		if allRecipientsObserved && now.Sub(lastChange) >= quietWindow {
			return true
		}
		if now.After(deadline) {
			return false
		}
		time.Sleep(100 * time.Microsecond)
	}
}

func TestRoomOnDataMessageUnlabeledLegacyDelivery(t *testing.T) {
	const legacyProtocol = types.ProtocolVersion(17)

	room, participants := newUnlabeledBroadcastRoom(t, 4, legacyProtocol)
	t.Cleanup(func() { room.Close(types.ParticipantCloseReasonNone) })

	source := participants[0]
	recorders := make([]*unlabeledSendRecorder, 0, len(participants)-1)
	for _, participant := range participants[1:] {
		recorder := &unlabeledSendRecorder{capture: true}
		participant.SendDataMessageUnlabeledCalls(recorder.record)
		recorders = append(recorders, recorder)
	}

	payloads := [][]byte{
		unlabeledBenchmarkPayload(1),
		unlabeledBenchmarkPayload(2),
		unlabeledBenchmarkPayload(3),
	}
	for _, payload := range payloads {
		room.onDataMessageUnlabeled(source, payload)
	}

	if got := source.SendDataMessageUnlabeledCallCount(); got != 0 {
		t.Fatalf("source received %d unlabeled sends, want 0", got)
	}
	for recipient, recorder := range recorders {
		sends := recorder.snapshot()
		if len(sends) != len(payloads) {
			t.Fatalf("recipient %d received %d sends, want %d", recipient, len(sends), len(payloads))
		}
		for i, send := range sends {
			if send.useRaw {
				t.Fatalf("recipient %d send %d used raw data channel", recipient, i)
			}
			if send.sender != source.Identity() {
				t.Fatalf("recipient %d send %d sender = %q, want %q", recipient, i, send.sender, source.Identity())
			}
			if string(send.data) != string(payloads[i]) {
				t.Fatalf("recipient %d send %d payload changed", recipient, i)
			}
		}
	}
}

func BenchmarkRoomOnDataMessageUnlabeledBurst(b *testing.B) {
	// Protocol 18 is reserved by this benchmark as the capable cohort. The base
	// server has no batching implementation, so it takes the current one-record
	// route; a future capability-gated implementation must preserve the same
	// legacy check above while reducing sends for this cohort.
	room, participants := newUnlabeledBroadcastRoom(b, unlabeledBenchmarkRecipients+1, types.ProtocolVersion(18))
	b.Cleanup(func() { room.Close(types.ParticipantCloseReasonNone) })

	source := participants[0]
	recorders := make([]*unlabeledSendRecorder, 0, unlabeledBenchmarkRecipients)
	for _, participant := range participants[1:] {
		recorder := &unlabeledSendRecorder{}
		participant.SendDataMessageUnlabeledCalls(recorder.record)
		recorders = append(recorders, recorder)
	}

	var sequence uint64
	var totalSends uint64
	var totalRecords uint64

	b.ResetTimer()
	for b.Loop() {
		for _, recorder := range recorders {
			recorder.reset()
		}

		for i := 0; i < unlabeledBenchmarkBurstMessages; i++ {
			room.onDataMessageUnlabeled(source, unlabeledBenchmarkPayload(sequence))
			sequence++
		}

		if !waitForUnlabeledFanoutQuiescence(recorders, 50*time.Millisecond) {
			b.Fatalf("fanout did not reach every recipient within the delivery bound")
		}

		var burstSends uint64
		var checksum uint64
		for _, recorder := range recorders {
			burstSends += recorder.calls.Load()
			checksum += recorder.checksum.Load()
		}
		if checksum == 0 {
			b.Fatal("fanout result was not consumed")
		}

		totalSends += burstSends
		totalRecords += unlabeledBenchmarkBurstMessages
	}
	b.StopTimer()

	b.ReportMetric(float64(totalSends)/float64(totalRecords), "recipient-sends/record")
}
