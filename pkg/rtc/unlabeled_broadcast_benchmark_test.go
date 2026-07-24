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

	"google.golang.org/protobuf/proto"

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
	records  atomic.Uint64
	checksum atomic.Uint64

	mu       sync.Mutex
	expected uint64
	active   bool
	done     chan struct{}
	sends    []recordedUnlabeledSend
}

func (r *unlabeledSendRecorder) begin(expected uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active {
		panic("unlabeled send recorder iteration still active")
	}
	r.expected = expected
	r.records.Store(0)
	r.active = true
	r.done = make(chan struct{})
}

func (r *unlabeledSendRecorder) wait() {
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	<-done
}

func (r *unlabeledSendRecorder) record(data []byte, useRaw bool, sender livekit.ParticipantIdentity) error {
	r.calls.Add(1)
	r.recordLogical(data, useRaw, sender)
	return nil
}

func (r *unlabeledSendRecorder) recordDataPacket(kind livekit.DataPacket_Kind, data []byte, _ livekit.ParticipantID, _ uint32) error {
	if kind != livekit.DataPacket_RELIABLE {
		return fmt.Errorf("batch data packet kind = %v", kind)
	}

	packet := &livekit.DataPacket{}
	if err := proto.Unmarshal(data, packet); err != nil {
		return err
	}
	user := packet.GetUser()
	if user == nil || user.Topic == nil || *user.Topic != UnlabeledBatchTopic {
		return fmt.Errorf("missing unlabeled batch topic")
	}
	records, err := UnmarshalUnlabeledBatch(user.Payload)
	if err != nil {
		return err
	}

	r.calls.Add(1)
	for _, record := range records {
		r.recordLogical(record, false, livekit.ParticipantIdentity(packet.ParticipantIdentity))
	}
	return nil
}

func (r *unlabeledSendRecorder) recordLogical(data []byte, useRaw bool, sender livekit.ParticipantIdentity) {
	var checksum uint64
	for _, value := range data {
		checksum += uint64(value)
	}
	r.checksum.Add(checksum)

	r.mu.Lock()
	if r.capture {
		r.sends = append(r.sends, recordedUnlabeledSend{
			data:   append([]byte(nil), data...),
			useRaw: useRaw,
			sender: sender,
		})
	}
	if r.active && r.records.Add(1) == r.expected {
		close(r.done)
		r.active = false
	}
	r.mu.Unlock()
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

func TestRoomOnDataMessageUnlabeledCapableDelivery(t *testing.T) {
	room, participants := newUnlabeledBroadcastRoom(t, 4, types.ProtocolVersionBatchedUnlabeled)
	t.Cleanup(func() { room.Close(types.ParticipantCloseReasonNone) })

	source := participants[0]
	recorders := make([]*unlabeledSendRecorder, 0, len(participants)-1)
	for _, participant := range participants[1:] {
		recorder := &unlabeledSendRecorder{capture: true}
		participant.SendDataMessageCalls(recorder.recordDataPacket)
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

	if got := source.SendDataMessageCallCount(); got != 0 {
		t.Fatalf("source received %d data sends, want 0", got)
	}
	for recipient, recorder := range recorders {
		if got := participants[recipient+1].SendDataMessageUnlabeledCallCount(); got != 0 {
			t.Fatalf("recipient %d received %d legacy sends, want 0", recipient, got)
		}
		sends := recorder.snapshot()
		if len(sends) != len(payloads) {
			t.Fatalf("recipient %d received %d callbacks, want %d", recipient, len(sends), len(payloads))
		}
		for i, send := range sends {
			if send.useRaw {
				t.Fatalf("recipient %d callback %d used raw data channel", recipient, i)
			}
			if send.sender != source.Identity() {
				t.Fatalf("recipient %d callback %d sender = %q, want %q", recipient, i, send.sender, source.Identity())
			}
			if string(send.data) != string(payloads[i]) {
				t.Fatalf("recipient %d callback %d payload changed", recipient, i)
			}
		}
	}
}

func benchmarkRoomOnDataMessageUnlabeledBurst(b *testing.B, protocol types.ProtocolVersion) {
	room, participants := newUnlabeledBroadcastRoom(b, unlabeledBenchmarkRecipients+1, protocol)
	b.Cleanup(func() { room.Close(types.ParticipantCloseReasonNone) })

	source := participants[0]
	recorders := make([]*unlabeledSendRecorder, 0, unlabeledBenchmarkRecipients)
	for _, participant := range participants[1:] {
		recorder := &unlabeledSendRecorder{}
		if protocol.SupportsBatchedUnlabeled() {
			participant.SendDataMessageCalls(recorder.recordDataPacket)
		} else {
			participant.SendDataMessageUnlabeledCalls(recorder.record)
		}
		recorders = append(recorders, recorder)
	}

	var sequence uint64
	for b.Loop() {
		for _, recorder := range recorders {
			recorder.begin(unlabeledBenchmarkBurstMessages)
		}
		for i := 0; i < unlabeledBenchmarkBurstMessages; i++ {
			room.onDataMessageUnlabeled(source, unlabeledBenchmarkPayload(sequence))
			sequence++
		}
		for _, recorder := range recorders {
			recorder.wait()
		}
	}
	b.StopTimer()

	var totalSends uint64
	var checksum uint64
	for _, recorder := range recorders {
		totalSends += recorder.calls.Load()
		checksum += recorder.checksum.Load()
	}
	if checksum == 0 {
		b.Fatal("fanout callbacks were not consumed")
	}

	totalRecords := uint64(b.N * unlabeledBenchmarkBurstMessages)
	b.ReportMetric(float64(totalSends)/float64(totalRecords), "recipient-sends/record")
}

func BenchmarkRoomOnDataMessageUnlabeledCapableBurst(b *testing.B) {
	// Each operation is a 16-record burst to the source-declared version-18
	// cohort. The recorder waits for actual decoded callbacks, not a scheduler
	// quiescence window, so deferred work cannot be reported as delivery.
	benchmarkRoomOnDataMessageUnlabeledBurst(b, types.ProtocolVersionBatchedUnlabeled)
}

func BenchmarkRoomOnDataMessageUnlabeledLegacyBurst(b *testing.B) {
	// This control uses the deployed version-17 route and the same payload and
	// recipient dimensions as the capable selector.
	benchmarkRoomOnDataMessageUnlabeledBurst(b, types.ProtocolVersion(17))
}
