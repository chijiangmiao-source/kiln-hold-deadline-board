package main

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Timer 是一次保温段倒计时的持久化记录。
// AcceptedAt 与 Deadline 均为 UTC 毫秒，创建后不可修改。
type Timer struct {
	ID         int64
	Label      string
	Minutes    int64
	AcceptedAt int64
	Deadline   int64
}

// ErrNotFound 表示计时不存在。
var ErrNotFound = errors.New("timer not found")

// Store 封装 SQLite 持久化。
type Store struct {
	db *sql.DB
}

const (
	schemaTable = `CREATE TABLE IF NOT EXISTS timers (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  label       TEXT    NOT NULL,
  minutes     INTEGER NOT NULL,
  accepted_at INTEGER NOT NULL,
  deadline    INTEGER NOT NULL
)`
	// 数据库层兜底：accepted_at 与 deadline 创建后不可修改。
	schemaTrigger = `CREATE TRIGGER IF NOT EXISTS timers_immutable
BEFORE UPDATE OF accepted_at, deadline ON timers
BEGIN
  SELECT RAISE(ABORT, 'accepted_at and deadline are immutable');
END`
)

// OpenStore 打开（必要时创建）SQLite 数据库并执行迁移。
func OpenStore(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite 单写者：串行化连接，避免 SQLITE_BUSY。
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{schemaTable, schemaTrigger} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate: %w", err)
		}
	}
	return &Store{db: db}, nil
}

// Close 关闭底层数据库。
func (s *Store) Close() error { return s.db.Close() }

// Ping 用于健康检查。
func (s *Store) Ping() error { return s.db.Ping() }

// CreateTimer 在写入 SQLite 的当下取 UTC 毫秒作为 accepted_at，
// 并在同一条 INSERT 中一次性写入 deadline = accepted_at + minutes*60000。
// 写库失败时返回错误，调用方不得据此给出任何计时标识。
func (s *Store) CreateTimer(label string, minutes int64) (Timer, error) {
	acceptedAt := time.Now().UTC().UnixMilli()
	deadline := acceptedAt + minutes*60000
	res, err := s.db.Exec(
		`INSERT INTO timers (label, minutes, accepted_at, deadline) VALUES (?, ?, ?, ?)`,
		label, minutes, acceptedAt, deadline,
	)
	if err != nil {
		return Timer{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Timer{}, err
	}
	return Timer{ID: id, Label: label, Minutes: minutes, AcceptedAt: acceptedAt, Deadline: deadline}, nil
}

// GetTimer 按 id 读取计时。
func (s *Store) GetTimer(id int64) (Timer, error) {
	var t Timer
	err := s.db.QueryRow(
		`SELECT id, label, minutes, accepted_at, deadline FROM timers WHERE id = ?`, id,
	).Scan(&t.ID, &t.Label, &t.Minutes, &t.AcceptedAt, &t.Deadline)
	if errors.Is(err, sql.ErrNoRows) {
		return Timer{}, ErrNotFound
	}
	if err != nil {
		return Timer{}, err
	}
	return t, nil
}

// ListTimers 按创建顺序返回全部计时。
func (s *Store) ListTimers() ([]Timer, error) {
	return s.queryTimers(`SELECT id, label, minutes, accepted_at, deadline FROM timers ORDER BY id`)
}

// ListTimersBoard 以 nowMs 为当前时刻，按看板顺序返回全部计时：
// 保温中（deadline > nowMs）的记录按截止时刻升序排在已到时记录之前，
// 同组内截止时刻相同则按 id 升序。临界毫秒（deadline == nowMs）归已到时组，
// 与 statusAt 的边界规则一致。排序键与 server 层的 boardLess 相同，
// SQLite 查询与 Go 服务共同保证该确定性顺序。
func (s *Store) ListTimersBoard(nowMs int64) ([]Timer, error) {
	return s.queryTimers(
		`SELECT id, label, minutes, accepted_at, deadline FROM timers
		 ORDER BY CASE WHEN deadline > ? THEN 0 ELSE 1 END, deadline, id`,
		nowMs,
	)
}

// queryTimers 执行查询并逐行扫描为计时切片。
func (s *Store) queryTimers(query string, args ...any) ([]Timer, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Timer
	for rows.Next() {
		var t Timer
		if err := rows.Scan(&t.ID, &t.Label, &t.Minutes, &t.AcceptedAt, &t.Deadline); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
