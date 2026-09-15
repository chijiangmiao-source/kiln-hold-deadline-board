package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) (*Server, *Store) {
	t.Helper()
	st, err := OpenStore(t.TempDir() + "/kiln.db")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewServer(st), st
}

func doRequest(t *testing.T, srv *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, r)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not JSON: %v (%q)", err, rec.Body.String())
	}
	return m
}

func intField(t *testing.T, m map[string]any, key string) int64 {
	t.Helper()
	v, ok := m[key].(float64)
	if !ok {
		t.Fatalf("field %q missing or not a number in %v", key, m)
	}
	return int64(v)
}

// TestStatusAtBoundary 验证临界规则：now<deadline 为 HOLDING，临界毫秒归 READY。
func TestStatusAtBoundary(t *testing.T) {
	const deadline = 1_000_000
	cases := []struct {
		now  int64
		want string
	}{
		{deadline - 1, "HOLDING"},
		{deadline, "READY"}, // 临界毫秒归 READY
		{deadline + 1, "READY"},
	}
	for _, c := range cases {
		if got := statusAt(c.now, deadline); got != c.want {
			t.Errorf("statusAt(%d, %d) = %s, want %s", c.now, deadline, got, c.want)
		}
	}
}

func TestRemainingMsClampsAtZero(t *testing.T) {
	if got := remainingMs(999_000, 1_000_000); got != 1000 {
		t.Errorf("remainingMs = %d, want 1000", got)
	}
	if got := remainingMs(1_000_000, 1_000_000); got != 0 {
		t.Errorf("remainingMs at deadline = %d, want 0", got)
	}
	if got := remainingMs(1_000_500, 1_000_000); got != 0 {
		t.Errorf("remainingMs past deadline = %d, want 0", got)
	}
}

func TestCreateValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	bad := []struct{ name, body string }{
		{"empty object", `{}`},
		{"empty label", `{"label":"","minutes":5}`},
		{"whitespace label", `{"label":"   ","minutes":5}`},
		{"label too long", `{"label":"` + strings.Repeat("窑", 81) + `","minutes":5}`},
		{"minutes zero", `{"label":"a","minutes":0}`},
		{"minutes negative", `{"label":"a","minutes":-3}`},
		{"minutes above max", `{"label":"a","minutes":181}`},
		{"minutes float", `{"label":"a","minutes":1.5}`},
		{"minutes string", `{"label":"a","minutes":"5"}`},
		{"minutes scientific", `{"label":"a","minutes":1e2}`},
		{"minutes missing", `{"label":"a"}`},
		{"unknown field", `{"label":"a","minutes":5,"extra":1}`},
		{"not json", `not-json`},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			rec := doRequest(t, srv, http.MethodPost, "/api/timers", c.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			m := decodeBody(t, rec)
			if _, ok := m["error"]; !ok {
				t.Fatalf("error body missing 'error': %v", m)
			}
			if _, ok := m["id"]; ok {
				t.Fatalf("error response must not carry a timer id: %v", m)
			}
		})
	}

	good := []struct{ name, body string }{
		{"min minutes", `{"label":"窑位-1","minutes":1}`},
		{"max minutes", `{"label":"窑位-2","minutes":180}`},
		{"label trimmed", `{"label":"  窑位-3  ","minutes":5}`},
		{"label 80 runes", `{"label":"` + strings.Repeat("窑", 80) + `","minutes":5}`},
	}
	for _, c := range good {
		t.Run(c.name, func(t *testing.T) {
			rec := doRequest(t, srv, http.MethodPost, "/api/timers", c.body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestCreateThenReadComputesFromServerNow 验证创建响应与后续读取的一致性：
// 时刻不可变，now/status/remaining_ms 按 API 当前时刻推导。
func TestCreateThenReadComputesFromServerNow(t *testing.T) {
	srv, _ := newTestServer(t)
	before := time.Now().UTC().UnixMilli()
	rec := doRequest(t, srv, http.MethodPost, "/api/timers", `{"label":"窑位B-1","minutes":5}`)
	after := time.Now().UTC().UnixMilli()
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	created := decodeBody(t, rec)

	accepted := intField(t, created, "accepted_at")
	deadline := intField(t, created, "deadline")
	if accepted < before || accepted > after {
		t.Fatalf("accepted_at %d outside [%d, %d]", accepted, before, after)
	}
	if deadline-accepted != 5*60000 {
		t.Fatalf("deadline-accepted_at = %d, want 300000", deadline-accepted)
	}
	if created["status"] != "HOLDING" {
		t.Fatalf("fresh timer status = %v, want HOLDING", created["status"])
	}

	id := intField(t, created, "id")
	rec2 := doRequest(t, srv, http.MethodGet, fmt.Sprintf("/api/timers/%d", id), "")
	if rec2.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec2.Code)
	}
	got := decodeBody(t, rec2)
	if intField(t, got, "deadline") != deadline || intField(t, got, "accepted_at") != accepted {
		t.Fatalf("timestamps changed between reads: %v vs %v", created, got)
	}
	now := intField(t, got, "now")
	if now < accepted {
		t.Fatalf("now %d < accepted_at %d", now, accepted)
	}
	if rem := intField(t, got, "remaining_ms"); rem != remainingMs(now, deadline) {
		t.Fatalf("remaining_ms %d inconsistent with now %d / deadline %d", rem, now, deadline)
	}
	if got["status"] != statusAt(now, deadline) {
		t.Fatalf("status %v inconsistent with now %d / deadline %d", got["status"], now, deadline)
	}
}

// TestGetReflectsDeadlinePosition 已过期为 READY 且剩余为 0，未到期为 HOLDING。
func TestGetReflectsDeadlinePosition(t *testing.T) {
	srv, st := newTestServer(t)
	now := time.Now().UTC().UnixMilli()
	insert := func(label string, acceptedAt, deadline int64) int64 {
		t.Helper()
		res, err := st.db.Exec(
			`INSERT INTO timers (label, minutes, accepted_at, deadline) VALUES (?, 1, ?, ?)`,
			label, acceptedAt, deadline,
		)
		if err != nil {
			t.Fatalf("insert %s: %v", label, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("LastInsertId: %v", err)
		}
		return id
	}
	pastID := insert("past", now-120000, now-60000)
	futureID := insert("future", now, now+3600000)

	rec := doRequest(t, srv, http.MethodGet, fmt.Sprintf("/api/timers/%d", pastID), "")
	m := decodeBody(t, rec)
	if m["status"] != "READY" || intField(t, m, "remaining_ms") != 0 {
		t.Fatalf("past deadline: got %v", m)
	}

	rec = doRequest(t, srv, http.MethodGet, fmt.Sprintf("/api/timers/%d", futureID), "")
	m = decodeBody(t, rec)
	if m["status"] != "HOLDING" {
		t.Fatalf("future deadline: got %v", m)
	}
	if rem := intField(t, m, "remaining_ms"); rem <= 0 || rem > 3600000 {
		t.Fatalf("remaining_ms %d out of range", rem)
	}
}

func TestGetNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doRequest(t, srv, http.MethodGet, "/api/timers/9999", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestCreateFailureReturnsErrorWithoutID 写库失败必须返回错误且不得给出计时标识。
func TestCreateFailureReturnsErrorWithoutID(t *testing.T) {
	srv, st := newTestServer(t)
	if err := st.Close(); err != nil { // 强制后续写入失败
		t.Fatalf("close: %v", err)
	}
	rec := doRequest(t, srv, http.MethodPost, "/api/timers", `{"label":"窑位C-1","minutes":5}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	m := decodeBody(t, rec)
	if _, ok := m["error"]; !ok {
		t.Fatalf("missing error: %v", m)
	}
	if _, ok := m["id"]; ok {
		t.Fatalf("failure response must not carry a timer id: %v", m)
	}
}

func TestListTimers(t *testing.T) {
	srv, _ := newTestServer(t)
	doRequest(t, srv, http.MethodPost, "/api/timers", `{"label":"L1","minutes":1}`)
	doRequest(t, srv, http.MethodPost, "/api/timers", `{"label":"L2","minutes":2}`)
	rec := doRequest(t, srv, http.MethodGet, "/api/timers", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var states []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &states); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("got %d timers, want 2", len(states))
	}
	for _, s := range states {
		if s["status"] != "HOLDING" {
			t.Fatalf("fresh timer status = %v, want HOLDING", s["status"])
		}
	}
}

// TestBoardLess 验证看板比较器的临界规则：保温中排前，临界毫秒归已到时，
// 同组按截止时刻升序，同截止按 id 升序。
func TestBoardLess(t *testing.T) {
	const now = int64(1_000_000)
	mk := func(id, deadline int64) Timer { return Timer{ID: id, Deadline: deadline} }
	cases := []struct {
		name string
		a, b Timer
		want bool
	}{
		{"holding before ready", mk(2, now+1), mk(1, now-1), true},
		{"ready never before holding", mk(1, now-1), mk(2, now+1), false},
		{"boundary ms is ready", mk(1, now), mk(2, now+1), false},
		{"holding just past boundary first", mk(2, now+1), mk(1, now), true},
		{"same group by deadline asc", mk(2, now+200), mk(1, now+100), false},
		{"same group by deadline asc 2", mk(2, now+100), mk(1, now+200), true},
		{"ready group by deadline asc", mk(1, now-200), mk(2, now-100), true},
		{"tie deadline by id asc", mk(1, now+100), mk(2, now+100), true},
		{"tie deadline id desc loses", mk(2, now+100), mk(1, now+100), false},
		{"tie deadline ready group by id", mk(1, now-5), mk(2, now-5), true},
	}
	for _, c := range cases {
		if got := boardLess(now, c.a, c.b); got != c.want {
			t.Errorf("%s: boardLess(%+v, %+v) = %v, want %v", c.name, c.a, c.b, got, c.want)
		}
	}
}

// TestListBoardView 多条保温中与已到时记录按看板顺序返回：
// 保温中按截止时刻升序排前，同截止按 id 升序；全部记录共用同一个服务端 now。
func TestListBoardView(t *testing.T) {
	srv, st := newTestServer(t)
	now := time.Now().UTC().UnixMilli()
	insert := func(label string, deadline int64) int64 {
		t.Helper()
		res, err := st.db.Exec(
			`INSERT INTO timers (label, minutes, accepted_at, deadline) VALUES (?, 1, ?, ?)`,
			label, now-120000, deadline,
		)
		if err != nil {
			t.Fatalf("insert %s: %v", label, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("LastInsertId: %v", err)
		}
		return id
	}
	// 乱序插入；截止时刻取整分钟，避免请求期间漂移改变分组。
	readyOld := insert("ready-old", now-60000)
	holdingLate := insert("holding-late", now+120000)
	readyRecent := insert("ready-recent", now-1000)
	holdingSoon := insert("holding-soon", now+60000)
	holdingSoon2 := insert("holding-soon-2", now+60000) // 同截止 → id 升序

	rec := doRequest(t, srv, http.MethodGet, "/api/timers?view=board", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var states []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &states); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []int64{holdingSoon, holdingSoon2, holdingLate, readyOld, readyRecent}
	if len(states) != len(want) {
		t.Fatalf("got %d timers, want %d", len(states), len(want))
	}
	var serverNow int64
	for i, id := range want {
		if got := intField(t, states[i], "id"); got != id {
			t.Fatalf("position %d: got id %d, want %d (full: %v)", i, got, id, states)
		}
		n := intField(t, states[i], "now")
		if i == 0 {
			serverNow = n
		} else if n != serverNow {
			t.Fatalf("now differs across board items: %d vs %d", n, serverNow)
		}
		deadline := intField(t, states[i], "deadline")
		if states[i]["status"] != statusAt(n, deadline) {
			t.Fatalf("item %d status %v inconsistent with now %d / deadline %d",
				i, states[i]["status"], n, deadline)
		}
		if rem := intField(t, states[i], "remaining_ms"); rem != remainingMs(n, deadline) {
			t.Fatalf("item %d remaining_ms %d inconsistent with now %d / deadline %d",
				i, rem, n, deadline)
		}
	}
	// 分组边界与采样 now 一致：保温中的截止时刻必须严格大于 now。
	if d := intField(t, states[2], "deadline"); d <= serverNow {
		t.Fatalf("holding deadline %d not after now %d", d, serverNow)
	}
	if d := intField(t, states[3], "deadline"); d > serverNow {
		t.Fatalf("ready deadline %d not at or before now %d", d, serverNow)
	}
}

// TestListBoardViewEmpty 看板视图表为空时返回空数组而非 null。
func TestListBoardViewEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doRequest(t, srv, http.MethodGet, "/api/timers?view=board", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Fatalf("empty board body = %q, want []", body)
	}
}

// TestListWithoutViewKeepsCreationOrder 未带 view=board 时（含未知 view 值）
// 保持原有响应与创建顺序，即使截止时刻顺序与创建顺序相反。
func TestListWithoutViewKeepsCreationOrder(t *testing.T) {
	srv, st := newTestServer(t)
	now := time.Now().UTC().UnixMilli()
	insert := func(label string, deadline int64) int64 {
		t.Helper()
		res, err := st.db.Exec(
			`INSERT INTO timers (label, minutes, accepted_at, deadline) VALUES (?, 1, ?, ?)`,
			label, now-120000, deadline,
		)
		if err != nil {
			t.Fatalf("insert %s: %v", label, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("LastInsertId: %v", err)
		}
		return id
	}
	// 先插入的记录截止更晚、甚至已到时：若误用看板排序会被重排。
	first := insert("first", now+3600000)
	second := insert("second", now-60000) // 已到时，看板视图会排到最后
	third := insert("third", now+60000)

	for _, target := range []string{"/api/timers", "/api/timers?view=unknown"} {
		rec := doRequest(t, srv, http.MethodGet, target, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", target, rec.Code)
		}
		var states []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &states); err != nil {
			t.Fatalf("%s: decode: %v", target, err)
		}
		want := []int64{first, second, third}
		if len(states) != len(want) {
			t.Fatalf("%s: got %d timers, want %d", target, len(states), len(want))
		}
		for i, id := range want {
			if got := intField(t, states[i], "id"); got != id {
				t.Fatalf("%s: position %d got id %d, want %d (creation order)", target, i, got, id)
			}
		}
	}
}

// TestListBoardFailureReturnsErrorWithoutID 看板读取失败只返回错误，不得携带计时标识。
func TestListBoardFailureReturnsErrorWithoutID(t *testing.T) {
	srv, st := newTestServer(t)
	if err := st.Close(); err != nil { // 强制后续读取失败
		t.Fatalf("close: %v", err)
	}
	rec := doRequest(t, srv, http.MethodGet, "/api/timers?view=board", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	m := decodeBody(t, rec)
	if _, ok := m["error"]; !ok {
		t.Fatalf("missing error: %v", m)
	}
	if _, ok := m["id"]; ok {
		t.Fatalf("failure response must not carry a timer id: %v", m)
	}
}

func TestHealth(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := doRequest(t, srv, http.MethodGet, "/api/health", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
