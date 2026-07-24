// Copyright 2026 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package test

import (
	"bytes"
	"testing"
	"time"

	testclient "github.com/livekit/livekit-server/test/client"
)

func TestUnlabeledDataDelivery(t *testing.T) {
	testUnlabeledDataDelivery(t, nil, nil)
}

func TestUnlabeledDataLegacyDelivery(t *testing.T) {
	testUnlabeledDataDelivery(t, legacyUnlabeledClientOptions(), legacyUnlabeledClientOptions())
}

func testUnlabeledDataDelivery(t *testing.T, publisherOptions, recipientOptions *testclient.Options) {
	t.Helper()
	_, finish := setupSingleNodeTest(t.Name())
	defer finish()

	publisher := createRTCClient("unlabeled-publisher", defaultServerPort, testRTCServicePathv0, publisherOptions)
	recipient := createRTCClient("unlabeled-recipient", defaultServerPort, testRTCServicePathv0, recipientOptions)
	defer publisher.Stop()
	defer recipient.Stop()
	waitUntilConnected(t, publisher, recipient)

	payload := []byte("unlabeled-data-payload")
	received := make(chan []byte, 1)
	recipient.OnDataReceived = func(data []byte, _ string) {
		received <- append([]byte(nil), data...)
	}
	if err := publisher.PublishDataUnlabeled(payload); err != nil {
		t.Fatal(err)
	}

	select {
	case data := <-received:
		if !bytes.Equal(data, payload) {
			t.Fatalf("payload = %q, want %q", data, payload)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("did not receive unlabeled data")
	}
}
