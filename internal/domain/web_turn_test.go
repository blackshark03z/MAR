package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCompactedWebTurnRetainsHashBasedIntegrity(t *testing.T) {
	request := json.RawMessage(`{"request_id":"r-compact","messages":[{"role":"user","content":"large prompt"}]}`)
	requestHash, err := HashWebTurnJSON(request)
	if err != nil {
		t.Fatal(err)
	}
	turn := WebTurn{ID: "turn-compact", TaskID: "task-compact", AttemptID: "attempt-compact", RunEpoch: 1, RequestID: "r-compact", Request: request, RequestHash: requestHash, CreatedAt: time.Unix(2, 0).UTC()}
	turn.IntegrityHash, err = turn.IntegrityDigest()
	if err != nil {
		t.Fatal(err)
	}
	turn.Request = nil
	turn.RequestCompacted = true
	if !turn.IntegrityValid() {
		t.Fatal("compacted web turn lost hash-based integrity")
	}
	turn.RequestHash = ""
	if turn.IntegrityValid() {
		t.Fatal("compacted web turn accepted missing original request hash")
	}
}

func TestWebTurnIntegritySurvivesEquivalentJSONReserialization(t *testing.T) {
	requestA := json.RawMessage(`{"request_id":"r1","model":"gpt-5.6-sol","messages":[{"role":"user","content":"fix it"}]}`)
	requestB := json.RawMessage(`{ "messages" : [ { "content":"fix it", "role":"user" } ], "model":"gpt-5.6-sol", "request_id":"r1" }`)
	hashA, err := HashWebTurnJSON(requestA)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := HashWebTurnJSON(requestB)
	if err != nil {
		t.Fatal(err)
	}
	if hashA != hashB {
		t.Fatalf("equivalent JSON must have one semantic hash: %s != %s", hashA, hashB)
	}
	turn := WebTurn{ID: "turn-1", TaskID: "task-1", AttemptID: "attempt-1", RunEpoch: 1, RequestID: "r1", Request: requestA, RequestHash: hashA, CreatedAt: time.Unix(1, 0).UTC()}
	turn.IntegrityHash, err = turn.IntegrityDigest()
	if err != nil {
		t.Fatal(err)
	}
	turn.Request = requestB
	if !turn.IntegrityValid() {
		t.Fatal("equivalent MCP JSON reserialization invalidated web turn integrity")
	}
	turn.Request = json.RawMessage(`{"request_id":"r1","model":"different","messages":[{"role":"user","content":"fix it"}]}`)
	if turn.IntegrityValid() {
		t.Fatal("semantic request mutation must invalidate web turn integrity")
	}
}
