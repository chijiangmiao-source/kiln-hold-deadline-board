package main

import (
	"strings"
	"testing"
	"time"
)

func openTempStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := t.TempDir() + "/kiln.db"
	st, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, path
}

func TestCreateTimerStampsAcceptedAtAndDeadline(t *testing.T) {
	st, _ := openTempStore(t)
	before := time.Now().UTC().UnixMilli()
	tm, err := st.CreateTimer("窑位A-1", 5)
	after := time.Now().UTC().UnixMilli()
	if err != nil {
		t.Fatalf("CreateTimer: %v", err)
	}
	if tm.AcceptedAt < before || tm.AcceptedAt > after {
		t.Fatalf("accepted_at %d not within [%d, %d]", tm.AcceptedAt, before, after)
	}
	if want := tm.AcceptedAt + 5*60000; tm.Deadline != want {
		t.Fatalf("deadline = %d, want %d (accepted_at + 5*60000)", tm.Deadline, want)
	}
	// 数据库中的行与返回值一致：accepted_at 与 deadline 是一次性写入的。
	row, err := st.GetTimer(tm.ID)
	if err != nil {
		t.Fatalf("GetTimer: %v", err)
	}
	if row != tm {
		t.Fatalf("stored row %+v != returned %+v", row, tm)
	}
}

func TestAcceptedAtAndDeadlineAreImmutable(t *testing.T) {
	st, _ := openTempStore(t)
	tm, err := st.CreateTimer("窑位A-2", 1)
	if err != nil {
		t.Fatalf("CreateTimer: %v", err)
	}
	for _, col := range []string{"accepted_at", "deadline"} {
		_, err := st.db.Exec("UPDATE timers SET "+col+" = "+col+" + 1 WHERE id = ?", tm.ID)
		if err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("update %s: expected immutability error, got %v", col, err)
		}
	}
	row, err := st.GetTimer(tm.ID)
	if err != nil {
		t.Fatalf("GetTimer: %v", err)
	}
	if row.AcceptedAt != tm.AcceptedAt || row.Deadline != tm.Deadline {
		t.Fatalf("timestamps drifted: %+v vs %+v", row, tm)
	}
}

// TestPersistenceAcrossReopen 模拟 API 重启：同一数据库文件重开后，
// 开始与截止时刻必须原样读回，不得漂移。
func TestPersistenceAcrossReopen(t *testing.T) {
	st, path := openTempStore(t)
	tm, err := st.CreateTimer("窑位A-3", 90)
	if err != nil {
		t.Fatalf("CreateTimer: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	row, err := reopened.GetTimer(tm.ID)
	if err != nil {
		t.Fatalf("GetTimer after reopen: %v", err)
	}
	if row.AcceptedAt != tm.AcceptedAt || row.Deadline != tm.Deadline {
		t.Fatalf("timestamps drifted across restart: before=%+v after=%+v", tm, row)
	}
}

func TestGetTimerNotFound(t *testing.T) {
	st, _ := openTempStore(t)
	if _, err := st.GetTimer(424242); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestListTimersBoardDeterministicOrder 以固定 now 验证看板顺序：
// 保温中（deadline > now）按截止时刻升序排在已到时之前，临界毫秒
// （deadline == now）归已到时组，同组截止时刻相同按 id 升序。
func TestListTimersBoardDeterministicOrder(t *testing.T) {
	st, _ := openTempStore(t)
	const now = int64(1_800_000_000_000)
	insert := func(label string, deadline int64) int64 {
		t.Helper()
		res, err := st.db.Exec(
			`INSERT INTO timers (label, minutes, accepted_at, deadline) VALUES (?, 1, ?, ?)`,
			label, now-60000, deadline,
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
	// 乱序插入，覆盖临界毫秒与同截止时刻的并列。
	readyBoundary := insert("ready-boundary", now) // 临界毫秒归已到时
	holdingLate := insert("holding-late", now+2000)
	readyEarly := insert("ready-early", now-1)
	holdingSoon := insert("holding-soon", now+1000)
	readyEarly2 := insert("ready-early-2", now-1)      // 与 readyEarly 同截止
	holdingSoon2 := insert("holding-soon-2", now+1000) // 与 holdingSoon 同截止

	got, err := st.ListTimersBoard(now)
	if err != nil {
		t.Fatalf("ListTimersBoard: %v", err)
	}
	want := []int64{holdingSoon, holdingSoon2, holdingLate, readyEarly, readyEarly2, readyBoundary}
	if len(got) != len(want) {
		t.Fatalf("got %d timers, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("position %d: got id %d, want %d (full order: %+v)", i, got[i].ID, id, got)
		}
	}

	// 旧版列表仍按创建顺序（id 升序），不受看板排序影响。
	legacy, err := st.ListTimers()
	if err != nil {
		t.Fatalf("ListTimers: %v", err)
	}
	for i := 1; i < len(legacy); i++ {
		if legacy[i-1].ID >= legacy[i].ID {
			t.Fatalf("legacy list not in creation order: %+v", legacy)
		}
	}
}
