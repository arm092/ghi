// Independent idiomatic Go counterpart to the measured Request Journal read routes.
// Writes and migration startup are outside the timing workload; the runner supplies
// identical databases created by the real Ghi service and seeds both copies equally.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

type Request struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}
type Event struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}
type FieldViolation struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}
type appError struct {
	status  int
	message string
	fields  []FieldViolation
}

func (e *appError) Error() string { return e.message }
func notFound() error             { return &appError{status: 404, message: "request not found"} }
func invalid(field, code, message string) error {
	return &appError{422, message, []FieldViolation{{field, code}}}
}

type repository struct{ db *sql.DB }

func (repo *repository) Get(ctx context.Context, id int64) (Request, error) {
	rows, err := repo.db.QueryContext(ctx, "SELECT id, title, status, created_at, updated_at FROM requests WHERE id = ?", id)
	if err != nil {
		return Request{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Request{}, err
		}
		return Request{}, notFound()
	}
	var item Request
	if err := rows.Scan(&item.ID, &item.Title, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Request{}, err
	}
	return item, rows.Close()
}
func (repo *repository) List(ctx context.Context, status string, limit, offset int) ([]Request, error) {
	rows, err := repo.db.QueryContext(ctx, "SELECT id, title, status, created_at, updated_at FROM requests WHERE (? = '' OR status = ?) ORDER BY id DESC LIMIT ? OFFSET ?", status, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Request{}
	for rows.Next() {
		var item Request
		if err := rows.Scan(&item.ID, &item.Title, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (repo *repository) History(ctx context.Context, id int64) ([]Event, error) {
	if _, err := repo.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := repo.db.QueryContext(ctx, "SELECT id, status, created_at FROM request_events WHERE request_id = ? ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.Status, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

type service struct{ repo *repository }

func (s *service) List(ctx context.Context, status string, limit, offset int) ([]Request, error) {
	violations := []FieldViolation{}
	if status != "" && status != "open" && status != "in_progress" && status != "resolved" {
		violations = append(violations, FieldViolation{"status", "one_of"})
	}
	if limit < 1 {
		violations = append(violations, FieldViolation{"limit", "min_value"})
	}
	if limit > 100 {
		violations = append(violations, FieldViolation{"limit", "max_value"})
	}
	if offset < 0 {
		violations = append(violations, FieldViolation{"offset", "min_value"})
	}
	if len(violations) > 0 {
		return nil, &appError{422, "invalid request fields", violations}
	}
	return s.repo.List(ctx, status, limit, offset)
}
func (s *service) Get(ctx context.Context, id int64) (Request, error) { return s.repo.Get(ctx, id) }
func (s *service) History(ctx context.Context, id int64) ([]Event, error) {
	return s.repo.History(ctx, id)
}
func send(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
func guard(next func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := next(w, r.WithContext(ctx)); err != nil {
			var problem *appError
			if errors.As(err, &problem) {
				if problem.fields != nil {
					send(w, problem.status, map[string]any{"error": problem.message, "fields": problem.fields})
				} else {
					send(w, problem.status, map[string]string{"error": problem.message})
				}
			} else {
				slog.Error("request_failed", "method", r.Method, "path", r.URL.Path, "error", err.Error())
				send(w, 500, map[string]string{"error": "internal server error"})
			}
		}
	}
}
func id(r *http.Request) (int64, error) {
	n, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || n < 1 {
		return 0, invalid("id", "positive_integer", "id must be a positive integer")
	}
	return n, nil
}
func number(value string, fallback int, field string) (int, error) {
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, invalid(field, "integer", "pagination parameters must be integers")
	}
	return n, nil
}
func router(s *service) http.Handler {
	r := chi.NewRouter()
	r.Get("/health", guard(func(w http.ResponseWriter, r *http.Request) error {
		if err := s.repo.db.PingContext(r.Context()); err != nil {
			return err
		}
		send(w, 200, map[string]string{"status": "ok"})
		return nil
	}))
	r.Get("/requests", guard(func(w http.ResponseWriter, r *http.Request) error {
		q := r.URL.Query()
		limit, err := number(q.Get("limit"), 20, "limit")
		if err != nil {
			return err
		}
		offset, err := number(q.Get("offset"), 0, "offset")
		if err != nil {
			return err
		}
		items, err := s.List(r.Context(), q.Get("status"), limit, offset)
		if err != nil {
			return err
		}
		send(w, 200, items)
		return nil
	}))
	r.Get("/requests/{id}", guard(func(w http.ResponseWriter, r *http.Request) error {
		key, err := id(r)
		if err != nil {
			return err
		}
		item, err := s.Get(r.Context(), key)
		if err != nil {
			return err
		}
		send(w, 200, item)
		return nil
	}))
	r.Get("/requests/{id}/history", guard(func(w http.ResponseWriter, r *http.Request) error {
		key, err := id(r)
		if err != nil {
			return err
		}
		items, err := s.History(r.Context(), key)
		if err != nil {
			return err
		}
		send(w, 200, items)
		return nil
	}))
	return r
}
func main() {
	absolute, err := filepath.Abs(os.Getenv("JOURNAL_DB"))
	if err != nil {
		panic(err)
	}
	connection := url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(absolute), "/"), RawQuery: "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", connection.String())
	if err != nil {
		panic(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		panic(err)
	}
	server := &http.Server{Addr: os.Getenv("JOURNAL_ADDR"), Handler: router(&service{&repository{db}}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		deadline, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(deadline); err != nil {
			server.Close()
		}
		close(done)
	}()
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	<-done
}
