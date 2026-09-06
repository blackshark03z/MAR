package domain

import (
	"encoding/json"
	"testing"
	"time"
)

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
