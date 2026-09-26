// Read-only counterpart to the measured users routes of examples/ddd-api.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type appError struct {
	status, code  int
	kind, message string
}

func (e *appError) Error() string { return e.message }

type user struct{ id, name, email string }

func newUser(id, name, email string) (*user, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 100 {
		return nil, errors.New("invalid name")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if len(email) > 254 {
		return nil, errors.New("invalid email")
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil {
		return nil, err
	}
	if parsed.Address != email {
		return nil, errors.New("invalid email")
	}
	return &user{id, name, email}, nil
}
func (u *user) Id() string    { return u.id }
func (u *user) Name() string  { return u.name }
func (u *user) Email() string { return u.email }

type userRepository interface {
	Find(context.Context, string) (*user, error)
	Page(context.Context, int, int) ([]*user, error)
}
type repository struct{ db *sql.DB }

func (repo *repository) Find(ctx context.Context, id string) (*user, error) {
	var storedID, name, email string
	err := repo.db.QueryRowContext(ctx, "SELECT id, name, email FROM users WHERE id = ?", id).Scan(&storedID, &name, &email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, &appError{404, 2002, "not_found", "user " + id + " was not found"}
	}
	if err != nil {
		return nil, err
	}
	return newUser(storedID, name, email)
}
func (repo *repository) Page(ctx context.Context, limit, offset int) ([]*user, error) {
	if limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		return nil, errors.New("invalid pagination bounds")
	}
	rows, err := repo.db.QueryContext(ctx, "SELECT id, name, email FROM users ORDER BY rowid LIMIT ? OFFSET ?", limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []*user{}
	for rows.Next() {
		var id, name, email string
		if err := rows.Scan(&id, &name, &email); err != nil {
			return nil, err
		}
		item, err := newUser(id, name, email)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

type service struct{ repo userRepository }

func (s *service) Get(ctx context.Context, id string) (*user, error) { return s.repo.Find(ctx, id) }
func (s *service) Page(ctx context.Context, limit, offset int) ([]*user, error) {
	return s.repo.Page(ctx, limit, offset)
}

type response struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func userResponse(u *user) response { return response{u.Id(), u.Name(), u.Email()} }
func send(w http.ResponseWriter, status int, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, err = w.Write(body)
	return err
}
func pagination(w http.ResponseWriter, r *http.Request) (int, int, error) {
	query := r.URL.Query()
	limit, offset := 50, 0
	for _, key := range []string{"limit", "offset"} {
		values := query[key]
		if len(values) > 1 {
			return 0, 0, errors.New("duplicate pagination")
		}
		if len(values) == 1 {
			value, err := strconv.Atoi(values[0])
			if err != nil {
				return 0, 0, err
			}
			if key == "limit" {
				limit = value
			} else {
				offset = value
			}
		}
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		return 0, 0, errors.New("invalid pagination")
	}
	w.Header().Set("X-Limit", strconv.Itoa(limit))
	w.Header().Set("X-Offset", strconv.Itoa(offset))
	return limit, offset, nil
}

type controller struct{ service *service }

func (c *controller) List(w http.ResponseWriter, r *http.Request) error {
	limit, offset, err := pagination(w, r)
	if err != nil {
		return err
	}
	items, err := c.service.Page(r.Context(), limit, offset)
	if err != nil {
		return err
	}
	result := []response{}
	for _, item := range items {
		result = append(result, userResponse(item))
	}
	return send(w, 200, result)
}
func (c *controller) Get(w http.ResponseWriter, r *http.Request) error {
	id, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		return err
	}
	item, err := c.service.Get(r.Context(), id.String())
	if err != nil {
		return err
	}
	return send(w, 200, userResponse(item))
}
func guarded(handler func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := handler(w, r); err != nil {
			failure := &appError{500, 500, "internal_error", "An internal error occurred"}
			if e, ok := err.(*appError); ok {
				failure = e
			}
			_ = send(w, failure.status, struct {
				Error struct {
					Code    int    `json:"code"`
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}{Error: struct {
				Code    int    `json:"code"`
				Type    string `json:"type"`
				Message string `json:"message"`
			}{failure.code, failure.kind, failure.message}})
		}
	}
}
func requestLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			response := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				status := response.Status()
				if status == 0 {
					status = 200
				}
				logger.Info("http_request", "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(started).Milliseconds(), "request_id", middleware.GetReqID(r.Context()))
			}()
			next.ServeHTTP(response, r)
		})
	}
}
func main() {
	db, err := sql.Open("sqlite", os.Getenv("GHI_DDD_DB")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		panic(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	c := &controller{&service{&repository{db}}}
	router := chi.NewRouter()
	router.Use(middleware.RequestID, requestLog(logger), middleware.Recoverer, middleware.Timeout(15*time.Second))
	router.Get("/health", guarded(func(w http.ResponseWriter, r *http.Request) error {
		return send(w, 200, map[string]string{"status": "ok"})
	}))
	router.Route("/api/v1/users", func(routes chi.Router) {
		routes.Get("/", guarded(c.List))
		routes.Route("/{userID}", func(item chi.Router) { item.Get("/", guarded(c.Get)) })
	})
	server := &http.Server{Addr: os.Getenv("GHI_DDD_ADDR"), Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError)}
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
