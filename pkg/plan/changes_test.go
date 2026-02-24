package plan

import (
	"testing"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
)

func ep(name, target, rt string) *endpoint.Endpoint {
	return endpoint.New(name, []string{target}, rt, 300, nil)
}

func TestChanges_IsEmpty_True(t *testing.T) {
	if !(&Changes{}).IsEmpty() {
		t.Error("zero-value Changes should be empty")
	}
}

func TestChanges_IsEmpty_Create(t *testing.T) {
	c := &Changes{Create: []*endpoint.Endpoint{ep("a.example.com", "1.1.1.1", endpoint.RecordTypeA)}}
	if c.IsEmpty() {
		t.Error("Changes with Create entries should not be empty")
	}
}

func TestChanges_IsEmpty_UpdateOld(t *testing.T) {
	c := &Changes{UpdateOld: []*endpoint.Endpoint{ep("a.example.com", "1.1.1.1", endpoint.RecordTypeA)}}
	if c.IsEmpty() {
		t.Error("Changes with UpdateOld entries should not be empty")
	}
}

func TestChanges_IsEmpty_UpdateNew(t *testing.T) {
	c := &Changes{UpdateNew: []*endpoint.Endpoint{ep("a.example.com", "2.2.2.2", endpoint.RecordTypeA)}}
	if c.IsEmpty() {
		t.Error("Changes with UpdateNew entries should not be empty")
	}
}

func TestChanges_IsEmpty_Delete(t *testing.T) {
	c := &Changes{Delete: []*endpoint.Endpoint{ep("a.example.com", "1.1.1.1", endpoint.RecordTypeA)}}
	if c.IsEmpty() {
		t.Error("Changes with Delete entries should not be empty")
	}
}
