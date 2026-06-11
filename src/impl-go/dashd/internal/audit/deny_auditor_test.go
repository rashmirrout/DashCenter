// PD-G4 denial auditing: DenyAuditor closure tests.
package audit

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestDenyAuditor_Nil(t *testing.T) {
	if DenyAuditor(nil) != nil {
		t.Fatal("DenyAuditor(nil) must return nil so middleware skips the call")
	}
}

func TestDenyAuditor_AppendsDenyEntry(t *testing.T) {
	w, dir := newWriter(t)
	da := DenyAuditor(w)
	if da == nil {
		t.Fatal("DenyAuditor returned nil for non-nil writer")
	}

	da("/api/v1/foo", 401, "anonymous", errors.New("missing token"))
	da("/api/v1/bar", 403, "cn:viewer-1", errors.New("permission denied"))

	entries := readAllEntries(t, filepath.Join(dir, "audit.jsonl"))
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}

	e0 := entries[0]
	if e0.OK || e0.Code != "Unauthenticated" || e0.Actor != "anonymous" ||
		e0.Method != "/api/v1/foo" || e0.Error != "missing token" {
		t.Errorf("401 entry mismatch: %+v", e0)
	}

	e1 := entries[1]
	if e1.OK || e1.Code != "PermissionDenied" || e1.Actor != "cn:viewer-1" ||
		e1.Method != "/api/v1/bar" {
		t.Errorf("403 entry mismatch: %+v", e1)
	}
}

func TestDenyAuditor_NilError(t *testing.T) {
	w, dir := newWriter(t)
	DenyAuditor(w)("/api/v1/x", 401, "anonymous", nil)

	entries := readAllEntries(t, filepath.Join(dir, "audit.jsonl"))
	if len(entries) != 1 || entries[0].Error != "" {
		t.Fatalf("nil err must leave Error empty: %+v", entries)
	}
}
