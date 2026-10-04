package call_handler

import (
	"net/http"
	"strings"
	"testing"
	"time"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	call_history "github.com/lucasgiovannibr/whatygo/pkg/call/history"
)

// fakeHistory is a call history that remembers what it was asked.
type fakeHistory struct {
	page      call_history.Page
	err       error
	instance  string
	query     call_history.Query
	deleted   int64
	delBefore time.Time
	delCalls  int
}

func (f *fakeHistory) Save(call_history.Record) error { return nil }
func (f *fakeHistory) List(instance string, q call_history.Query) (call_history.Page, error) {
	f.instance, f.query = instance, q
	return f.page, f.err
}
func (f *fakeHistory) Delete(instance string, before time.Time) (int64, error) {
	f.instance, f.delBefore = instance, before
	f.delCalls++
	return f.deleted, f.err
}
func (f *fakeHistory) Purge(time.Time) (int64, error) { return 0, nil }

func TestHistoryIsA409WhenTheServerKeepsNone(t *testing.T) {
	e := newEnv(t)
	if w := e.call("GET", "/call/history", "inst", nil); w.Code != http.StatusConflict {
		t.Fatalf("GET: %d %s", w.Code, w.Body.String())
	}
	if w := e.call("DELETE", "/call/history", "inst", nil); w.Code != http.StatusConflict {
		t.Fatalf("DELETE: %d %s", w.Code, w.Body.String())
	}
}

func TestHistoryListsTheCallsOfTheCallersInstance(t *testing.T) {
	start := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	answered := start.Add(6 * time.Second)
	repo := &fakeHistory{page: call_history.Page{
		Records: []call_history.Record{{
			Id: "r1", InstanceID: "inst", CallID: "c1", Peer: "273117121392855@lid", PeerPhone: "5531999990000",
			Direction: "incoming", Outcome: "answered", Reason: "peer_hangup",
			StartedAt: start, AnsweredAt: &answered, EndedAt: start.Add(66 * time.Second), TalkSeconds: 60, RingSeconds: 6,
		}},
		Next: "abc",
	}}
	e := newEnvWithHistory(t, call_engine.Options{}, repo)

	w := e.call("GET", "/call/history?direction=incoming&outcome=answered&peer=%2B5531999990000&limit=25&cursor=xyz", "inst", nil)
	var got call_history.Page
	decode(t, w, &got)
	if w.Code != http.StatusOK || len(got.Records) != 1 || got.Next != "abc" {
		t.Fatalf("answer: %d %s", w.Code, w.Body.String())
	}
	if r := got.Records[0]; r.CallID != "c1" || r.PeerPhone != "5531999990000" || r.TalkSeconds != 60 || r.AnsweredAt == nil {
		t.Fatalf("record = %+v", r)
	}
	if repo.instance != "inst" {
		t.Fatalf("the history of instance %q was read, want the caller's", repo.instance)
	}
	if q := repo.query; q.Direction != "incoming" || q.Outcome != "answered" || q.Peer != "+5531999990000" || q.Limit != 25 || q.Cursor != "xyz" {
		t.Fatalf("query = %+v", q)
	}
	// the instance is not part of the record it hands out: the caller already knows it
	if body := w.Body.String(); strings.Contains(body, "instance") {
		t.Fatalf("the answer names the instance: %s", body)
	}
}

func TestHistoryTakesTheDefaultsWhenNothingIsAsked(t *testing.T) {
	repo := &fakeHistory{}
	e := newEnvWithHistory(t, call_engine.Options{}, repo)
	if w := e.call("GET", "/call/history", "inst", nil); w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if repo.query != (call_history.Query{}) {
		t.Fatalf("query = %+v, want the zero query", repo.query)
	}
}

func TestHistoryRefusesABadRequestWith400(t *testing.T) {
	repo := &fakeHistory{err: call_history.ErrInvalidQuery}
	e := newEnvWithHistory(t, call_engine.Options{}, repo)
	for _, path := range []string{
		"/call/history?limit=abc",
		"/call/history?limit=0",
		"/call/history?limit=-3",
		"/call/history?outcome=great", // the repository refuses it
	} {
		if w := e.call("GET", path, "inst", nil); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (%s)", path, w.Code, w.Body.String())
		}
	}
}

func TestADatabaseErrorIsA500(t *testing.T) {
	repo := &fakeHistory{err: http.ErrAbortHandler}
	e := newEnvWithHistory(t, call_engine.Options{}, repo)
	if w := e.call("GET", "/call/history", "inst", nil); w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", w.Code)
	}
}

func TestDeleteErasesTheHistoryOfTheCallersInstance(t *testing.T) {
	repo := &fakeHistory{deleted: 12}
	e := newEnvWithHistory(t, call_engine.Options{}, repo)

	w := e.call("DELETE", "/call/history", "inst", nil)
	var got struct{ Deleted int64 }
	decode(t, w, &got)
	if w.Code != http.StatusOK || got.Deleted != 12 {
		t.Fatalf("answer: %d %s", w.Code, w.Body.String())
	}
	if repo.instance != "inst" || !repo.delBefore.IsZero() {
		t.Fatalf("deleted instance %q before %v, want the caller's, all of it", repo.instance, repo.delBefore)
	}

	w = e.call("DELETE", "/call/history?before=2026-09-01T00:00:00Z", "inst", nil)
	if w.Code != http.StatusOK || !repo.delBefore.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("before: %d, deleted before %v", w.Code, repo.delBefore)
	}
}

// A mistyped date must not turn "erase what is older than this" into "erase everything".
func TestDeleteWithABadDateErasesNothing(t *testing.T) {
	repo := &fakeHistory{deleted: 99}
	e := newEnvWithHistory(t, call_engine.Options{}, repo)
	for _, before := range []string{"yesterday", "2026-09-01", "1696118400", "2026-13-45T00:00:00Z"} {
		if w := e.call("DELETE", "/call/history?before="+before, "inst", nil); w.Code != http.StatusBadRequest {
			t.Errorf("before=%s: status %d, want 400", before, w.Code)
		}
	}
	if repo.delCalls != 0 {
		t.Fatalf("the history was erased %d times by a request with a bad date", repo.delCalls)
	}
}
