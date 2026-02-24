package fake

import (
	"context"
	"testing"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
	"github.com/bkero/external-dns-docker/pkg/plan"
)

func ep(name, target, rt string) *endpoint.Endpoint {
	return endpoint.New(name, []string{target}, rt, 300, nil)
}

func TestNew_PreloadsRecords(t *testing.T) {
	p := New([]*endpoint.Endpoint{
		ep("a.example.com", "1.2.3.4", endpoint.RecordTypeA),
		ep("b.example.com", "5.6.7.8", endpoint.RecordTypeA),
	})

	recs, err := p.Records(context.Background())
	if err != nil {
		t.Fatalf("Records() error = %v", err)
	}
	if len(recs) != 2 {
		t.Errorf("got %d records, want 2", len(recs))
	}
}

func TestRecords_EmptyProvider(t *testing.T) {
	p := New(nil)
	recs, err := p.Records(context.Background())
	if err != nil {
		t.Fatalf("Records() error = %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("got %d records, want 0", len(recs))
	}
}

func TestApplyChanges_Create(t *testing.T) {
	p := New(nil)

	err := p.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{ep("new.example.com", "1.1.1.1", endpoint.RecordTypeA)},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}

	recs, _ := p.Records(context.Background())
	if len(recs) != 1 {
		t.Fatalf("got %d records after create, want 1", len(recs))
	}
	if recs[0].DNSName != "new.example.com" {
		t.Errorf("DNSName = %q, want new.example.com", recs[0].DNSName)
	}
}

func TestApplyChanges_Delete(t *testing.T) {
	initial := ep("old.example.com", "9.9.9.9", endpoint.RecordTypeA)
	p := New([]*endpoint.Endpoint{initial})

	err := p.ApplyChanges(context.Background(), &plan.Changes{
		Delete: []*endpoint.Endpoint{initial},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}

	if p.RecordCount() != 0 {
		t.Errorf("got %d records after delete, want 0", p.RecordCount())
	}
}

func TestApplyChanges_Update(t *testing.T) {
	old := ep("app.example.com", "1.2.3.4", endpoint.RecordTypeA)
	p := New([]*endpoint.Endpoint{old})

	newEp := ep("app.example.com", "5.6.7.8", endpoint.RecordTypeA)
	err := p.ApplyChanges(context.Background(), &plan.Changes{
		UpdateOld: []*endpoint.Endpoint{old},
		UpdateNew: []*endpoint.Endpoint{newEp},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}

	recs, _ := p.Records(context.Background())
	if len(recs) != 1 {
		t.Fatalf("got %d records after update, want 1", len(recs))
	}
	if recs[0].Targets[0] != "5.6.7.8" {
		t.Errorf("target after update = %q, want 5.6.7.8", recs[0].Targets[0])
	}
}

func TestApplyChanges_RecordedInHistory(t *testing.T) {
	p := New(nil)

	if err := p.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{ep("a.example.com", "1.1.1.1", endpoint.RecordTypeA)},
	}); err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}
	if err := p.ApplyChanges(context.Background(), &plan.Changes{
		Delete: []*endpoint.Endpoint{ep("a.example.com", "1.1.1.1", endpoint.RecordTypeA)},
	}); err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}

	h := p.History()
	if len(h) != 2 {
		t.Fatalf("history length = %d, want 2", len(h))
	}
	if len(h[0].Create) != 1 {
		t.Errorf("first call Create len = %d, want 1", len(h[0].Create))
	}
	if len(h[1].Delete) != 1 {
		t.Errorf("second call Delete len = %d, want 1", len(h[1].Delete))
	}
}

func TestApplyChanges_EmptyChanges_RecordedInHistory(t *testing.T) {
	p := New(nil)
	if err := p.ApplyChanges(context.Background(), &plan.Changes{}); err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}

	if len(p.History()) != 1 {
		t.Errorf("expected empty change to still be recorded in history")
	}
}

func TestChanges_IsEmpty(t *testing.T) {
	if !(&plan.Changes{}).IsEmpty() {
		t.Error("empty Changes.IsEmpty() should be true")
	}
	c := &plan.Changes{Create: []*endpoint.Endpoint{ep("a.example.com", "1.1.1.1", endpoint.RecordTypeA)}}
	if c.IsEmpty() {
		t.Error("non-empty Changes.IsEmpty() should be false")
	}
}

func TestRecordCount(t *testing.T) {
	p := New([]*endpoint.Endpoint{
		ep("a.example.com", "1.1.1.1", endpoint.RecordTypeA),
		ep("b.example.com", "2.2.2.2", endpoint.RecordTypeA),
	})
	if p.RecordCount() != 2 {
		t.Errorf("RecordCount() = %d, want 2", p.RecordCount())
	}
}
