package state

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMCPRequestIdempotencyAndExecutionDeliverySeparation(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	record := MCPRequestRecord{
		RequestID: "REQ123", SourceIdentity: "slack:C1:1.000", SourceMessageID: "slack:C1:1.000",
		Method: "mcpServer/tool/call", ArgumentsHash: "hash-a", RequestJSON: `{"request_id":"REQ123"}`,
		ExecutionState: MCPExecutionReceived, DeliveryState: MCPDeliveryPending, CreatedAt: 10, UpdatedAt: 10,
	}
	stored, inserted, err := store.InsertMCPRequestIfAbsent(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	if !inserted || stored.RequestID != "REQ123" || stored.ExecutionState != MCPExecutionReceived || stored.DeliveryState != MCPDeliveryPending {
		t.Fatalf("stored = %#v inserted=%v", stored, inserted)
	}
	if err := store.UpdateMCPExecution(ctx, "REQ123", MCPExecutionPreparing, 15); err != nil {
		t.Fatal(err)
	}
	stored, err = store.MCPRequestByID(ctx, "REQ123")
	if err != nil || stored.ExecutionState != MCPExecutionPreparing || stored.DeliveryState != MCPDeliveryPending {
		t.Fatalf("execution update changed the wrong fact: stored=%#v err=%v", stored, err)
	}

	conflicting := record
	conflicting.SourceIdentity = "slack:C1:2.000"
	conflicting.SourceMessageID = "slack:C1:2.000"
	conflicting.ArgumentsHash = "hash-b"
	stored, inserted, err = store.InsertMCPRequestIfAbsent(ctx, conflicting)
	if err != nil {
		t.Fatal(err)
	}
	if inserted || stored.ArgumentsHash != "hash-a" || stored.SourceIdentity != record.SourceIdentity {
		t.Fatalf("duplicate changed stored fact: %#v inserted=%v", stored, inserted)
	}

	response := `{"schema":"cwapi.mcp.response.v1","status":"unavailable"}`
	if err := store.CompleteMCPRequest(ctx, "REQ123", "unavailable", response, 20); err != nil {
		t.Fatal(err)
	}
	stored, err = store.MCPRequestByID(ctx, "REQ123")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ExecutionState != "unavailable" || stored.ResponseJSON != response || stored.DeliveryState != MCPDeliveryPending {
		t.Fatalf("execution completion must not imply delivery: %#v", stored)
	}

	if err := store.UpdateMCPDelivery(ctx, "REQ123", MCPDeliveryDelivered, 30); err != nil {
		t.Fatal(err)
	}
	stored, err = store.MCPRequestByID(ctx, "REQ123")
	if err != nil {
		t.Fatal(err)
	}
	if stored.ExecutionState != "unavailable" || stored.DeliveryState != MCPDeliveryDelivered {
		t.Fatalf("delivery update changed execution fact: %#v", stored)
	}
}
