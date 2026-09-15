package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// maxLabelRunes 限制窑位标签长度，防止异常输入。
	maxLabelRunes = 80
	// maxBodyBytes 限制创建请求体大小。
	maxBodyBytes = 4096
	// minMinutes / maxMinutes 是保温分钟数的合法闭区间。
	minMinutes = 1
	maxMinutes = 180
)

// minutesPattern 只接受十进制整数字面量（拒绝小数、科学计数、符号与空白）。
var minutesPattern = regexp.MustCompile(`^[0-9]+$`)

// Server 持有 HTTP 处理器所需的依赖。
type Server struct {
	store *Store
}

// NewServer 构造 Server。
func NewServer(store *Store) *Server { return &Server{store: store} }

// Handler 返回带路由与 CORS 的处理器链。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/timers", s.handleCreate)
	mux.HandleFunc("GET /api/timers", s.handleList)
	mux.HandleFunc("GET /api/timers/{id}", s.handleGet)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	return withCORS(mux)
}

// timerState 是计时在“读取当下”的视图：状态与剩余毫秒只按 API 当前 UTC 毫秒推导。
type timerState struct {
	ID          int64  `json:"id"`
	Label       string `json:"label"`
	Minutes     int64  `json:"minutes"`
	AcceptedAt  int64  `json:"accepted_at"`
	Deadline    int64  `json:"deadline"`
	Now         int64  `json:"now"`
	Status      string `json:"status"`
	RemainingMs int64  `json:"remaining_ms"`
}

// statusAt 实现唯一状态规则：now < deadline 为 HOLDING，否则 READY（临界毫秒归 READY）。
func statusAt(nowMs, deadlineMs int64) string {
	if nowMs < deadlineMs {
		return "HOLDING"
	}
	return "READY"
}

// remainingMs 实现剩余毫秒规则：max(0, deadline-now)。
func remainingMs(nowMs, deadlineMs int64) int64 {
	if d := deadlineMs - nowMs; d > 0 {
		return d
	}
	return 0
}

// stateOf 以 nowMs 为“当前时刻”生成计时视图。
func stateOf(t Timer, nowMs int64) timerState {
	return timerState{
		ID:          t.ID,
		Label:       t.Label,
		Minutes:     t.Minutes,
		AcceptedAt:  t.AcceptedAt,
		Deadline:    t.Deadline,
		Now:         nowMs,
		Status:      statusAt(nowMs, t.Deadline),
		RemainingMs: remainingMs(nowMs, t.Deadline),
	}
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label   string          `json:"label"`
		Minutes json.RawMessage `json:"minutes"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "request body must be a JSON object with label and minutes")
		return
	}
	if dec.More() {
		writeError(w, http.StatusBadRequest, "request body must contain a single JSON object")
		return
	}

	label := strings.TrimSpace(req.Label)
	if label == "" {
		writeError(w, http.StatusBadRequest, "label must not be empty")
		return
	}
	if utf8.RuneCountInString(label) > maxLabelRunes {
		writeError(w, http.StatusBadRequest, "label must be at most 80 characters")
		return
	}
	// RawMessage 保留原始 JSON 令牌：字符串、小数、科学计数一律拒绝。
	raw := string(req.Minutes)
	if !minutesPattern.MatchString(raw) {
		writeError(w, http.StatusBadRequest, "minutes must be a decimal integer between 1 and 180")
		return
	}
	minutes, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || minutes < minMinutes || minutes > maxMinutes {
		writeError(w, http.StatusBadRequest, "minutes must be a decimal integer between 1 and 180")
		return
	}

	t, err := s.store.CreateTimer(label, minutes)
	if err != nil {
		// 写库失败：只返回错误，绝不携带计时标识。
		writeError(w, http.StatusInternalServerError, "failed to persist timer")
		return
	}
	writeJSON(w, http.StatusCreated, stateOf(t, time.Now().UTC().UnixMilli()))
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return
	}
	t, err := s.store.GetTimer(id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "timer not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read timer")
		return
	}
	writeJSON(w, http.StatusOK, stateOf(t, time.Now().UTC().UnixMilli()))
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	timers, err := s.store.ListTimers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read timers")
		return
	}
	now := time.Now().UTC().UnixMilli()
	states := make([]timerState, 0, len(timers))
	for _, t := range timers {
		states = append(states, stateOf(t, now))
	}
	writeJSON(w, http.StatusOK, states)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	if err := s.store.Ping(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// writeJSON 以给定状态码输出 JSON。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError 输出统一错误体；绝不携带计时标识。
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// withCORS 允许跨源访问；前端经反向代理同源访问时不受影响。
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
