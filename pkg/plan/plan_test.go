package plan

import (
	"sort"
	"testing"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
)

// helpers

func a(name, target string) *endpoint.Endpoint {
	return endpoint.New(name, []string{target}, endpoint.RecordTypeA, 300, nil)
}

func aTTL(name, target string, ttl int64) *endpoint.Endpoint {
	return endpoint.New(name, []string{target}, endpoint.RecordTypeA, ttl, nil)
}

func ownerTXT(name string) *endpoint.Endpoint {
	return endpoint.New(
		ownerPrefix+name,
		[]string{ownershipValue(DefaultOwnerID)},
		endpoint.RecordTypeTXT,
		ownershipTTL,
		nil,
	)
}

func ownerTXTID(name, ownerID string) *endpoint.Endpoint {
	return endpoint.New(
		ownerPrefix+name,
		[]string{ownershipValue(ownerID)},
		endpoint.RecordTypeTXT,
		ownershipTTL,
		nil,
	)
}

func plan() *Plan { return New(DefaultOwnerID) }

// sortedNames extracts DNSNames from a slice of endpoints, sorted for stable comparison.
func sortedNames(eps []*endpoint.Endpoint) []string {
	names := make([]string, len(eps))
	for i, ep := range eps {
		names[i] = ep.DNSName
	}
	sort.Strings(names)
	return names
}

// --- Create scenarios ---

func TestCalculate_NewRecord_ProducesCreate(t *testing.T) {
	desired := []*endpoint.Endpoint{a("app.example.com", "1.2.3.4")}
	current := []*endpoint.Endpoint{}

	changes := plan().Calculate(desired, current)

	if len(changes.Create) != 2 {
		t.Fatalf("Create len = %d, want 2 (record + ownership TXT)", len(changes.Create))
	}
	names := sortedNames(changes.Create)
	if names[0] != "app.example.com" {
		t.Errorf("Create[0].DNSName = %q, want app.example.com", names[0])
	}
	if names[1] != ownerPrefix+"app.example.com" {
		t.Errorf("Create[1].DNSName = %q, want %s", names[1], ownerPrefix+"app.example.com")
	}
	if len(changes.Delete) != 0 || len(changes.UpdateOld) != 0 {
		t.Errorf("unexpected deletes or updates: %+v", changes)
	}
}

func TestCalculate_NewRecord_OwnershipTXTValue(t *testing.T) {
	desired := []*endpoint.Endpoint{a("app.example.com", "1.2.3.4")}
	changes := plan().Calculate(desired, nil)

	var txt *endpoint.Endpoint
	for _, ep := range changes.Create {
		if ep.RecordType == endpoint.RecordTypeTXT {
			txt = ep
		}
	}
	if txt == nil {
		t.Fatal("no TXT ownership record in Create")
	}
	want := ownershipValue(DefaultOwnerID)
	if len(txt.Targets) != 1 || txt.Targets[0] != want {
		t.Errorf("ownership TXT value = %q, want %q", txt.Targets, want)
	}
}

// --- Delete scenarios ---

func TestCalculate_OwnedMissingRecord_ProducesDelete(t *testing.T) {
	desired := []*endpoint.Endpoint{}
	current := []*endpoint.Endpoint{
		a("old.example.com", "9.9.9.9"),
		ownerTXT("old.example.com"),
	}

	changes := plan().Calculate(desired, current)

	if len(changes.Delete) != 2 {
		t.Fatalf("Delete len = %d, want 2 (record + ownership TXT)", len(changes.Delete))
	}
	// Sort alphabetically: "external-dns-docker-owner.*" ('e') < "old.*" ('o')
	names := sortedNames(changes.Delete)
	if names[0] != ownerPrefix+"old.example.com" {
		t.Errorf("Delete[0] = %q, want %s", names[0], ownerPrefix+"old.example.com")
	}
	if names[1] != "old.example.com" {
		t.Errorf("Delete[1] = %q, want old.example.com", names[1])
	}
}

func TestCalculate_UnownedRecord_NotDeleted(t *testing.T) {
	desired := []*endpoint.Endpoint{}
	current := []*endpoint.Endpoint{
		a("manual.example.com", "1.2.3.4"),
		// no ownership TXT record
	}

	changes := plan().Calculate(desired, current)

	if len(changes.Delete) != 0 {
		t.Errorf("Delete len = %d, want 0 (unowned record must not be deleted)", len(changes.Delete))
	}
}

func TestCalculate_UnownedRecord_NotUpdated(t *testing.T) {
	// When a record exists in DNS but has no ownership TXT, we cannot update it.
	// We also do not create a competing record for the same name/type.
	// The desired record is effectively blocked until the manual record is removed.
	desired := []*endpoint.Endpoint{a("manual.example.com", "9.9.9.9")}
	current := []*endpoint.Endpoint{
		a("manual.example.com", "1.2.3.4"),
		// no ownership TXT record
	}

	changes := plan().Calculate(desired, current)

	if len(changes.UpdateOld) != 0 {
		t.Errorf("UpdateOld len = %d, want 0 (unowned record must not be updated)", len(changes.UpdateOld))
	}
	if len(changes.Create) != 0 {
		t.Errorf("Create len = %d, want 0 (cannot create competing record for unowned name)", len(changes.Create))
	}
}

// --- Update scenarios ---

func TestCalculate_ChangedTarget_ProducesUpdate(t *testing.T) {
	desired := []*endpoint.Endpoint{a("app.example.com", "5.6.7.8")}
	current := []*endpoint.Endpoint{
		a("app.example.com", "1.2.3.4"),
		ownerTXT("app.example.com"),
	}

	changes := plan().Calculate(desired, current)

	if len(changes.UpdateOld) != 1 || len(changes.UpdateNew) != 1 {
		t.Fatalf("UpdateOld=%d UpdateNew=%d, want 1 each", len(changes.UpdateOld), len(changes.UpdateNew))
	}
	if changes.UpdateOld[0].Targets[0] != "1.2.3.4" {
		t.Errorf("UpdateOld target = %q, want 1.2.3.4", changes.UpdateOld[0].Targets[0])
	}
	if changes.UpdateNew[0].Targets[0] != "5.6.7.8" {
		t.Errorf("UpdateNew target = %q, want 5.6.7.8", changes.UpdateNew[0].Targets[0])
	}
	if len(changes.Create) != 0 || len(changes.Delete) != 0 {
		t.Errorf("unexpected creates or deletes")
	}
}

func TestCalculate_ChangedTTL_ProducesUpdate(t *testing.T) {
	desired := []*endpoint.Endpoint{aTTL("app.example.com", "1.2.3.4", 600)}
	current := []*endpoint.Endpoint{
		aTTL("app.example.com", "1.2.3.4", 300),
		ownerTXT("app.example.com"),
	}

	changes := plan().Calculate(desired, current)

	if len(changes.UpdateOld) != 1 {
		t.Errorf("UpdateOld len = %d, want 1 (TTL change)", len(changes.UpdateOld))
	}
}

// --- No-change scenarios ---

func TestCalculate_UnchangedRecord_NoOp(t *testing.T) {
	ep := a("app.example.com", "1.2.3.4")
	desired := []*endpoint.Endpoint{ep}
	current := []*endpoint.Endpoint{
		a("app.example.com", "1.2.3.4"),
		ownerTXT("app.example.com"),
	}

	changes := plan().Calculate(desired, current)

	if !changes.IsEmpty() {
		t.Errorf("expected no changes for unchanged owned record, got %+v", changes)
	}
}

func TestCalculate_EmptyDesiredAndCurrent_Empty(t *testing.T) {
	changes := plan().Calculate(nil, nil)
	if !changes.IsEmpty() {
		t.Errorf("expected empty changes, got %+v", changes)
	}
}

// --- Multiple records ---

func TestCalculate_MixedScenario(t *testing.T) {
	desired := []*endpoint.Endpoint{
		a("new.example.com", "1.1.1.1"),       // create
		a("unchanged.example.com", "2.2.2.2"), // no-op
		a("changed.example.com", "9.9.9.9"),   // update (owned)
	}
	current := []*endpoint.Endpoint{
		a("unchanged.example.com", "2.2.2.2"),
		ownerTXT("unchanged.example.com"),
		a("changed.example.com", "3.3.3.3"),
		ownerTXT("changed.example.com"),
		a("deleted.example.com", "4.4.4.4"), // delete (owned)
		ownerTXT("deleted.example.com"),
		a("manual.example.com", "5.5.5.5"), // unowned — untouched
	}

	changes := plan().Calculate(desired, current)

	// new.example.com: record + TXT
	if len(changes.Create) != 2 {
		t.Errorf("Create len = %d, want 2", len(changes.Create))
	}
	// changed.example.com
	if len(changes.UpdateOld) != 1 || len(changes.UpdateNew) != 1 {
		t.Errorf("Update len = %d/%d, want 1/1", len(changes.UpdateOld), len(changes.UpdateNew))
	}
	// deleted.example.com: record + TXT
	if len(changes.Delete) != 2 {
		t.Errorf("Delete len = %d, want 2", len(changes.Delete))
	}
}

// --- Custom owner ID ---

func TestCalculate_CustomOwnerID(t *testing.T) {
	p := New("my-instance")
	desired := []*endpoint.Endpoint{}
	current := []*endpoint.Endpoint{
		a("app.example.com", "1.2.3.4"),
		ownerTXTID("app.example.com", "my-instance"),
	}

	changes := p.Calculate(desired, current)

	if len(changes.Delete) != 2 {
		t.Errorf("Delete len = %d, want 2 (custom owner matched)", len(changes.Delete))
	}
}

func TestCalculate_WrongOwnerID_NotDeleted(t *testing.T) {
	p := New("my-instance")
	desired := []*endpoint.Endpoint{}
	current := []*endpoint.Endpoint{
		a("app.example.com", "1.2.3.4"),
		ownerTXTID("app.example.com", "other-instance"),
	}

	changes := p.Calculate(desired, current)

	if len(changes.Delete) != 0 {
		t.Errorf("Delete len = %d, want 0 (wrong owner must not be deleted)", len(changes.Delete))
	}
}

// --- Multiple targets ---

func TestCalculate_MultipleTargets_EqualWhenSame(t *testing.T) {
	ep1 := endpoint.New("app.example.com", []string{"1.1.1.1", "2.2.2.2"}, endpoint.RecordTypeA, 300, nil)
	ep2 := endpoint.New("app.example.com", []string{"2.2.2.2", "1.1.1.1"}, endpoint.RecordTypeA, 300, nil) // different order

	if !endpointsEqual(ep1, ep2) {
		t.Error("endpoints with same targets in different order should be equal")
	}
}

func TestCalculate_MultipleTargets_NotEqualWhenDifferent(t *testing.T) {
	ep1 := endpoint.New("app.example.com", []string{"1.1.1.1", "2.2.2.2"}, endpoint.RecordTypeA, 300, nil)
	ep2 := endpoint.New("app.example.com", []string{"1.1.1.1", "3.3.3.3"}, endpoint.RecordTypeA, 300, nil)

	if endpointsEqual(ep1, ep2) {
		t.Error("endpoints with different targets should not be equal")
	}
}

// --- Helper unit tests ---

func TestOwnershipName(t *testing.T) {
	got := ownershipName("app.example.com")
	want := ownerPrefix + "app.example.com"
	if got != want {
		t.Errorf("ownershipName = %q, want %q", got, want)
	}
}

func TestOwnershipValue(t *testing.T) {
	got := ownershipValue("my-daemon")
	want := "heritage=external-dns-docker,external-dns-docker/owner=my-daemon"
	if got != want {
		t.Errorf("ownershipValue = %q, want %q", got, want)
	}
}

func TestNew_DefaultOwnerID(t *testing.T) {
	p := New("")
	if p.ownerID != DefaultOwnerID {
		t.Errorf("ownerID = %q, want %q", p.ownerID, DefaultOwnerID)
	}
}

func TestFilterOwnershipTXTs(t *testing.T) {
	eps := []*endpoint.Endpoint{
		a("app.example.com", "1.2.3.4"),
		ownerTXT("app.example.com"),
	}
	filtered := filterOwnershipTXTs(eps)
	if len(filtered) != 1 {
		t.Errorf("filtered len = %d, want 1", len(filtered))
	}
	if filtered[0].DNSName != "app.example.com" {
		t.Errorf("filtered[0].DNSName = %q, want app.example.com", filtered[0].DNSName)
	}
}

func TestBuildOwnedSet_NonOwnerPrefixTXT_Ignored(t *testing.T) {
	// A TXT record that exists but does not start with ownerPrefix must not
	// influence the owned-name set (covers the HasPrefix continue branch).
	p := plan()
	current := []*endpoint.Endpoint{
		// Plain TXT record, not an ownership sidecar.
		endpoint.New("app.example.com", []string{"some-value"}, endpoint.RecordTypeTXT, 300, nil),
		a("app.example.com", "1.2.3.4"),
	}
	owned := p.buildOwnedSet(current)
	if owned["app.example.com"] {
		t.Error("app.example.com should not be owned (TXT lacks ownerPrefix)")
	}
}

func TestEndpointsEqual_DifferentLengths_NotEqual(t *testing.T) {
	ep1 := endpoint.New("app.example.com", []string{"1.1.1.1", "2.2.2.2"}, endpoint.RecordTypeA, 300, nil)
	ep2 := endpoint.New("app.example.com", []string{"1.1.1.1"}, endpoint.RecordTypeA, 300, nil)

	if endpointsEqual(ep1, ep2) {
		t.Error("endpoints with different target counts should not be equal")
	}
}
