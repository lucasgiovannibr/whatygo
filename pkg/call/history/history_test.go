package call_history

import (
	"database/sql/driver"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New() // regexp matching of the SQL, the default
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, WithoutReturning: true}), &gorm.Config{SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

var columns = []string{"id", "instance_id", "call_id", "peer", "peer_phone", "direction", "video", "outcome", "reason",
	"started_at", "answered_at", "ended_at", "talk_seconds", "ring_seconds"}

func row(id string, started time.Time) []driver.Value {
	return []driver.Value{id, "inst", "call-" + id, "273117121392855@lid", "5531999990000", "incoming", false, "answered", "peer_hangup",
		started, started.Add(5 * time.Second), started.Add(65 * time.Second), 60, 5}
}

func TestSaveIgnoresACallThatIsAlreadyThere(t *testing.T) {
	db, mock := newDB(t)
	mock.ExpectExec(`INSERT INTO "call_records".*ON CONFLICT \("instance_id","call_id"\) DO NOTHING`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := NewRepository(db).Save(Record{InstanceID: "inst", CallID: "c1", Direction: "incoming", StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListScopesToTheInstanceAndPagesNewestFirst(t *testing.T) {
	db, mock := newDB(t)
	t0 := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows(columns).
		AddRow(row("c", t0)...).
		AddRow(row("b", t0.Add(-time.Hour))...).
		AddRow(row("a", t0.Add(-2*time.Hour))...) // the extra row that says there is a next page
	mock.ExpectQuery(`SELECT \* FROM "call_records" WHERE instance_id = \$1 ORDER BY started_at DESC, id DESC LIMIT \$2`).
		WithArgs("inst", 3).
		WillReturnRows(rows)

	page, err := NewRepository(db).List("inst", Query{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 2 || page.Records[0].Id != "c" || page.Records[1].Id != "b" {
		t.Fatalf("records = %+v", page.Records)
	}
	if page.Records[0].PeerPhone != "5531999990000" || page.Records[0].TalkSeconds != 60 {
		t.Fatalf("record = %+v", page.Records[0])
	}
	// the cursor continues after the last record of the page
	at, id, err := decodeCursor(page.Next)
	if err != nil || id != "b" || !at.Equal(t0.Add(-time.Hour)) {
		t.Fatalf("cursor = %v %q (%v)", at, id, err)
	}
}

func TestTheLastPageHasNoCursor(t *testing.T) {
	db, mock := newDB(t)
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(columns).AddRow(row("a", time.Now())...))
	page, err := NewRepository(db).List("inst", Query{Limit: 5})
	if err != nil || len(page.Records) != 1 || page.Next != "" {
		t.Fatalf("page = %+v (%v)", page, err)
	}
}

func TestAnEmptyHistoryIsAnEmptyListNotNull(t *testing.T) {
	db, mock := newDB(t)
	mock.ExpectQuery(`SELECT`).WillReturnRows(sqlmock.NewRows(columns))
	page, err := NewRepository(db).List("inst", Query{})
	if err != nil || page.Records == nil || len(page.Records) != 0 {
		t.Fatalf("page = %+v (%v)", page, err)
	}
}

func TestListFiltersAndContinuesFromACursor(t *testing.T) {
	db, mock := newDB(t)
	cursorAt := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	cursor := encodeCursor(cursorAt, "zzz")

	mock.ExpectQuery(`WHERE instance_id = \$1 AND direction = \$2 AND outcome = \$3 AND \(peer = \$4 OR peer_phone = \$5\) AND \(started_at, id\) < \(\$6, \$7\) ORDER BY started_at DESC, id DESC LIMIT \$8`).
		WithArgs("inst", "incoming", "missed", "+55 (31) 99999-0000", "5531999990000", cursorAt, "zzz", DefaultLimit+1).
		WillReturnRows(sqlmock.NewRows(columns))

	_, err := NewRepository(db).List("inst", Query{Direction: "incoming", Outcome: "missed", Peer: "+55 (31) 99999-0000", Cursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestListRefusesWhatItCannotAnswer(t *testing.T) {
	db, _ := newDB(t) // no query is expected: every one of these is refused before asking the database
	repo := NewRepository(db)
	for name, q := range map[string]Query{
		"direction":      {Direction: "sideways"},
		"outcome":        {Outcome: "great"},
		"limit too big":  {Limit: MaxLimit + 1},
		"limit negative": {Limit: -1},
		"cursor":         {Cursor: "not a cursor"},
		"cursor shape":   {Cursor: "MTIzNA"}, // base64 of "1234": no id
	} {
		if _, err := repo.List("inst", q); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("%s: err = %v, want ErrInvalidQuery", name, err)
		}
	}
}

func TestEveryOutcomeTheEngineProducesCanBeFiltered(t *testing.T) {
	for _, o := range call_engine.Outcomes {
		if !validOutcome(o) {
			t.Errorf("outcome %q cannot be filtered", o)
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 2, 9, 8, 7, 654321000, time.UTC)
	got, id, err := decodeCursor(encodeCursor(at, "9f0c"))
	if err != nil || id != "9f0c" || !got.Equal(at) {
		t.Fatalf("cursor = %v %q (%v)", got, id, err)
	}
}

func TestDeleteErasesOneInstanceAndOptionallyOnlyTheOldOnes(t *testing.T) {
	db, mock := newDB(t)
	repo := NewRepository(db)

	mock.ExpectExec(`DELETE FROM "call_records" WHERE instance_id = \$1$`).
		WithArgs("inst").WillReturnResult(sqlmock.NewResult(0, 7))
	if n, err := repo.Delete("inst", time.Time{}); err != nil || n != 7 {
		t.Fatalf("Delete all = %d, %v", n, err)
	}

	before := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectExec(`DELETE FROM "call_records" WHERE instance_id = \$1 AND started_at < \$2`).
		WithArgs("inst", before).WillReturnResult(sqlmock.NewResult(0, 2))
	if n, err := repo.Delete("inst", before); err != nil || n != 2 {
		t.Fatalf("Delete before = %d, %v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPurgeErasesEveryInstancesOldRecords(t *testing.T) {
	db, mock := newDB(t)
	cutoff := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	mock.ExpectExec(`DELETE FROM "call_records" WHERE started_at < \$1`).
		WithArgs(cutoff).WillReturnResult(sqlmock.NewResult(0, 11))
	if n, err := NewRepository(db).Purge(cutoff); err != nil || n != 11 {
		t.Fatalf("Purge = %d, %v", n, err)
	}
}

// --- the recorder

type fakeRepo struct {
	mu      sync.Mutex
	saved   []Record
	saveErr error
	panics  bool
	purges  atomic.Int32
	cutoffs []time.Time
}

func (f *fakeRepo) Save(r Record) error {
	if f.panics {
		panic("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, r)
	return nil
}
func (f *fakeRepo) List(string, Query) (Page, error)        { return Page{}, nil }
func (f *fakeRepo) Delete(string, time.Time) (int64, error) { return 0, nil }
func (f *fakeRepo) Purge(before time.Time) (int64, error) {
	f.purges.Add(1)
	f.mu.Lock()
	f.cutoffs = append(f.cutoffs, before)
	f.mu.Unlock()
	return 3, nil
}

type testLog struct {
	mu   sync.Mutex
	errs []string
	info []string
}

func (l *testLog) LogError(f string, a ...interface{}) {
	l.mu.Lock()
	l.errs = append(l.errs, f)
	l.mu.Unlock()
}
func (l *testLog) LogInfo(f string, a ...interface{}) {
	l.mu.Lock()
	l.info = append(l.info, f)
	l.mu.Unlock()
}

func syncRecorder(repo Repository, resolve PhoneResolver, log Logger) *Recorder {
	r := NewRecorder(repo, resolve, log)
	r.async = func(f func()) { f() }
	return r
}

func TestTheRecorderSavesWhatTheEngineReports(t *testing.T) {
	repo := &fakeRepo{}
	rec := syncRecorder(repo, func(instance, peer string) string { return "5531999990000" }, &testLog{})
	start := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	rec.Handle(call_engine.Record{
		InstanceID: "inst", CallID: "c1", Peer: "273117121392855@lid", Direction: call_engine.Incoming, Video: true,
		Outcome: call_engine.OutcomeAnswered, Reason: "peer_hangup",
		StartedAt: start, AnsweredAt: start.Add(7 * time.Second), EndedAt: start.Add(97 * time.Second),
	})

	if len(repo.saved) != 1 {
		t.Fatalf("%d saved", len(repo.saved))
	}
	got := repo.saved[0]
	if got.InstanceID != "inst" || got.CallID != "c1" || got.Peer != "273117121392855@lid" || got.PeerPhone != "5531999990000" ||
		got.Direction != "incoming" || !got.Video || got.Outcome != "answered" || got.Reason != "peer_hangup" ||
		got.TalkSeconds != 90 || got.RingSeconds != 7 || got.AnsweredAt == nil || !got.StartedAt.Equal(start) {
		t.Fatalf("saved = %+v", got)
	}
}

func TestACallThatWasNotAnsweredHasNoAnswerTime(t *testing.T) {
	repo := &fakeRepo{}
	syncRecorder(repo, nil, &testLog{}).Handle(call_engine.Record{
		InstanceID: "inst", CallID: "c2", Outcome: call_engine.OutcomeMissed,
		StartedAt: time.Now(), EndedAt: time.Now().Add(12 * time.Second),
	})
	if len(repo.saved) != 1 || repo.saved[0].AnsweredAt != nil || repo.saved[0].TalkSeconds != 0 || repo.saved[0].RingSeconds != 12 {
		t.Fatalf("saved = %+v", repo.saved)
	}
}

func TestADatabaseThatFailsLosesTheRecordAndSaysSo(t *testing.T) {
	log := &testLog{}
	syncRecorder(&fakeRepo{saveErr: errors.New("connection refused")}, nil, log).Handle(call_engine.Record{InstanceID: "inst", CallID: "c3"})
	if len(log.errs) != 1 {
		t.Fatalf("errors logged = %v, want one", log.errs)
	}
}

func TestARepositoryThatPanicsDoesNotTakeTheCallDown(t *testing.T) {
	log := &testLog{}
	syncRecorder(&fakeRepo{panics: true}, nil, log).Handle(call_engine.Record{InstanceID: "inst", CallID: "c4"})
	if len(log.errs) != 1 {
		t.Fatalf("errors logged = %v, want one", log.errs)
	}
}

func TestTheRecorderDoesNotMakeTheEndOfACallWait(t *testing.T) {
	block := make(chan struct{})
	repo := &blockingRepo{release: block}
	rec := NewRecorder(repo, nil, &testLog{}) // the real, asynchronous one

	done := make(chan struct{})
	go func() { rec.Handle(call_engine.Record{InstanceID: "inst", CallID: "c5"}); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handle waited for the database")
	}
	close(block)
}

type blockingRepo struct {
	fakeRepo
	release chan struct{}
}

func (b *blockingRepo) Save(r Record) error { <-b.release; return nil }

func TestPhoneOf(t *testing.T) {
	for jid, want := range map[string]string{
		"5531999990000@s.whatsapp.net":   "5531999990000",
		"5531999990000:7@s.whatsapp.net": "5531999990000", // a device
		"273117121392855@lid":            "",
		"120363012345678901@g.us":        "",
		"not a jid":                      "",
		"":                               "",
	} {
		if got := PhoneOf(jid); got != want {
			t.Errorf("PhoneOf(%q) = %q, want %q", jid, got, want)
		}
	}
}

func TestDigits(t *testing.T) {
	if got := digits("+55 (31) 99999-0000"); got != "5531999990000" {
		t.Fatalf("digits = %q", got)
	}
}

// --- retention

func TestRetainDeletesOldRecordsNowAndEveryInterval(t *testing.T) {
	repo := &fakeRepo{}
	stop := make(chan struct{})
	defer close(stop)

	before := time.Now()
	Retain(repo, 24*time.Hour, 20*time.Millisecond, &testLog{}, stop)

	deadline := time.Now().Add(3 * time.Second)
	for repo.purges.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("only %d purges", repo.purges.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	// the cutoff is "now minus the retention"
	if cut := repo.cutoffs[0]; cut.After(before.Add(-24*time.Hour+time.Second)) || cut.Before(before.Add(-24*time.Hour-time.Second)) {
		t.Fatalf("cutoff = %v, want about %v", cut, before.Add(-24*time.Hour))
	}
}

func TestRetainStopsWhenTold(t *testing.T) {
	repo := &fakeRepo{}
	stop := make(chan struct{})
	Retain(repo, time.Hour, 10*time.Millisecond, &testLog{}, stop)
	time.Sleep(60 * time.Millisecond)
	close(stop)
	time.Sleep(40 * time.Millisecond)
	n := repo.purges.Load()
	time.Sleep(80 * time.Millisecond)
	if repo.purges.Load() != n {
		t.Fatal("it kept purging after it was stopped")
	}
}

func TestZeroRetentionKeepsEverythingAndStartsNothing(t *testing.T) {
	repo := &fakeRepo{}
	Retain(repo, 0, 10*time.Millisecond, &testLog{}, make(chan struct{}))
	time.Sleep(60 * time.Millisecond)
	if repo.purges.Load() != 0 {
		t.Fatal("a history kept for ever was purged")
	}
}
