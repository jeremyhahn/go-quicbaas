// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package iam

import (
	"encoding/json"
	"testing"
)

func TestConsentStatus_MarshalUnmarshalJSON(t *testing.T) {
	data, err := json.Marshal(ConsentStatusWithdrawn)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `"withdrawn"` {
		t.Errorf("got %s", string(data))
	}

	strCases := map[string]ConsentStatus{
		"granted":   ConsentStatusGranted,
		"withdrawn": ConsentStatusWithdrawn,
		"expired":   ConsentStatusExpired,
	}
	for in, want := range strCases {
		var got ConsentStatus
		if err := json.Unmarshal([]byte(`"`+in+`"`), &got); err != nil {
			t.Fatalf("unmarshal %q: %v", in, err)
		}
		if got != want {
			t.Errorf("unmarshal %q = %v, want %v", in, got, want)
		}
	}

	var fromNum ConsentStatus
	if err := json.Unmarshal([]byte(`1`), &fromNum); err != nil {
		t.Fatalf("unmarshal number: %v", err)
	}
	if fromNum != ConsentStatusWithdrawn {
		t.Errorf("got %v from number", fromNum)
	}

	var bad ConsentStatus
	if err := json.Unmarshal([]byte(`"nope"`), &bad); err == nil {
		t.Error("expected error for unknown string")
	}
}

func TestExportFormat_MarshalUnmarshalJSON(t *testing.T) {
	data, err := json.Marshal(ExportFormatCSV)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `"csv"` {
		t.Errorf("got %s", string(data))
	}

	strCases := map[string]ExportFormat{
		"json": ExportFormatJSON,
		"csv":  ExportFormatCSV,
		"xml":  ExportFormatXML,
	}
	for in, want := range strCases {
		var got ExportFormat
		if err := json.Unmarshal([]byte(`"`+in+`"`), &got); err != nil {
			t.Fatalf("unmarshal %q: %v", in, err)
		}
		if got != want {
			t.Errorf("unmarshal %q = %v, want %v", in, got, want)
		}
	}

	var fromNum ExportFormat
	if err := json.Unmarshal([]byte(`2`), &fromNum); err != nil {
		t.Fatalf("unmarshal number: %v", err)
	}
	if fromNum != ExportFormatXML {
		t.Errorf("got %v from number", fromNum)
	}
}

func TestExportStatus_MarshalUnmarshalJSON(t *testing.T) {
	data, err := json.Marshal(ExportStatusPending)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `"pending"` {
		t.Errorf("got %s", string(data))
	}

	strCases := map[string]ExportStatus{
		"pending":    ExportStatusPending,
		"processing": ExportStatusProcessing,
		"completed":  ExportStatusCompleted,
		"failed":     ExportStatusFailed,
		"expired":    ExportStatusExpired,
	}
	for in, want := range strCases {
		var got ExportStatus
		if err := json.Unmarshal([]byte(`"`+in+`"`), &got); err != nil {
			t.Fatalf("unmarshal %q: %v", in, err)
		}
		if got != want {
			t.Errorf("unmarshal %q = %v, want %v", in, got, want)
		}
	}

	var fromNum ExportStatus
	if err := json.Unmarshal([]byte(`0`), &fromNum); err != nil {
		t.Fatalf("unmarshal number: %v", err)
	}
	if fromNum != ExportStatusPending {
		t.Errorf("got %v from number", fromNum)
	}
}

func TestDeletionStatus_MarshalUnmarshalJSON(t *testing.T) {
	data, err := json.Marshal(DeletionStatusScheduled)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `"scheduled"` {
		t.Errorf("got %s", string(data))
	}

	strCases := map[string]DeletionStatus{
		"pending":    DeletionStatusPending,
		"scheduled":  DeletionStatusScheduled,
		"processing": DeletionStatusProcessing,
		"completed":  DeletionStatusCompleted,
		"cancelled":  DeletionStatusCancelled,
		"failed":     DeletionStatusFailed,
	}
	for in, want := range strCases {
		var got DeletionStatus
		if err := json.Unmarshal([]byte(`"`+in+`"`), &got); err != nil {
			t.Fatalf("unmarshal %q: %v", in, err)
		}
		if got != want {
			t.Errorf("unmarshal %q = %v, want %v", in, got, want)
		}
	}

	var fromNum DeletionStatus
	if err := json.Unmarshal([]byte(`1`), &fromNum); err != nil {
		t.Fatalf("unmarshal number: %v", err)
	}
	if fromNum != DeletionStatusScheduled {
		t.Errorf("got %v from number", fromNum)
	}
}
