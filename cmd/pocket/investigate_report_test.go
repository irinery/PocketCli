package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"pocketcli/internal/investigation/annotations"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/queries"
	"pocketcli/internal/investigation/storage"
)

type fakeReport struct {
	calls     int
	artifacts map[string]contract.ReportArtifact
}

func (f *fakeReport) Finalize(_ context.Context, in contract.FinalizeRequest) (contract.ReportArtifact, error) {
	f.calls++
	a := contract.ReportArtifact{ID: fmt.Sprintf("report-%d", f.calls), SnapshotID: in.SnapshotID, Revision: f.calls, Status: "completed"}
	f.artifacts[a.ID] = a
	return a, nil
}
func (f *fakeReport) Get(id string) (contract.ReportArtifact, error) {
	a, ok := f.artifacts[id]
	if !ok {
		return a, os.ErrNotExist
	}
	return a, nil
}
func (f *fakeReport) List(snapshot string) []contract.ReportArtifact {
	out := []contract.ReportArtifact{}
	for _, a := range f.artifacts {
		if a.SnapshotID == snapshot {
			out = append(out, a)
		}
	}
	return out
}
func (*fakeReport) Open(string) (*os.File, error) { return nil, os.ErrNotExist }

func TestReportCacheFinalizesOncePerRevision(t *testing.T) {
	root := t.TempDir()
	store, err := storage.Open(filepath.Join(root, "store"))
	if err != nil {
		t.Fatal(err)
	}
	notes, err := annotations.Open(filepath.Join(root, "notes"))
	if err != nil {
		t.Fatal(err)
	}
	queryHistory, err := queries.Open(filepath.Join(root, "queries"))
	if err != nil {
		t.Fatal(err)
	}
	inner := &fakeReport{artifacts: make(map[string]contract.ReportArtifact)}
	cache := &reportCache{inner: inner, store: store, notes: notes, queries: queryHistory}
	request := contract.FinalizeRequest{SnapshotID: "snap-1"}
	first, err := cache.Finalize(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cache.Finalize(context.Background(), request)
	if err != nil || first.ID != second.ID || inner.calls != 1 {
		t.Fatalf("same revision rendered twice: first=%+v second=%+v calls=%d err=%v", first, second, inner.calls, err)
	}
	restarted := &reportCache{inner: inner, store: store, notes: notes, queries: queryHistory}
	third, err := restarted.Finalize(context.Background(), request)
	if err != nil || third.ID != first.ID || inner.calls != 1 {
		t.Fatalf("restart rendered twice: third=%+v calls=%d err=%v", third, inner.calls, err)
	}
	request.AnnotationRevision = 1
	fourth, err := restarted.Finalize(context.Background(), request)
	if err != nil || fourth.ID == first.ID || inner.calls != 2 {
		t.Fatalf("new revision not rendered: fourth=%+v calls=%d err=%v", fourth, inner.calls, err)
	}
}
