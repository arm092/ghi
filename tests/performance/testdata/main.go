package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type Counter struct{ value int }

func (c *Counter) add(n int) int { c.value += n; return c.value }

type Base struct{ value int }

func (c *Base) add(n int) int { c.value += n; return c.value }

type Derived struct{ Base }

func (c *Derived) add(n int) int { c.value += n * 2; return c.value }

type Adder interface{ add(int) int }
type Inherited struct{ Base }

func inherited(b *testing.B) {
	c := &Inherited{}
	for i := 0; i < b.N; i++ {
		sink = c.add(i)
	}
}

var escaped = &Counter{}

func fields(b *testing.B) {
	c := &Counter{}
	for i := 0; i < b.N; i++ {
		c.value += i
	}
	sink = c.value
}
func method(b *testing.B) {
	c := &Counter{}
	for i := 0; i < b.N; i++ {
		sink = c.add(i)
	}
}
func virtual(b *testing.B) {
	objects := []Adder{&Base{}, &Derived{}}
	for i := 0; i < b.N; i++ {
		sink = objects[i&1].add(i)
	}
}
func create(b *testing.B) {
	for i := 0; i < b.N; i++ {
		escaped = &Counter{value: i}
	}
	sink = escaped.value
}
func optional(value int) *int {
	if value&1 == 0 {
		return nil
	}
	return &value
}
func nullable(b *testing.B) {
	for i := 0; i < b.N; i++ {
		value := optional(i)
		if value != nil {
			sink = *value
		}
	}
}
func maybeFail(value int) (int, error) {
	if value < 0 {
		return 0, errors.New("failure")
	}
	return value, nil
}

var operation = maybeFail

func success(b *testing.B) {
	for i := 0; i < b.N; i++ {
		value, err := operation(i)
		if err != nil {
			sink = 7
		} else {
			sink = value
		}
	}
}
func failure(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if value, err := operation(-1); err != nil {
			sink = 7
		} else {
			sink = value
		}
	}
}

type Handler struct{}

func deepFailure(depth int) (int, error) {
	if depth == 0 {
		return operation(-1)
	}
	return deepFailure(depth - 1)
}
func failureDeep(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if value, err := deepFailure(96); err != nil {
			sink = 7
		} else {
			sink = value
		}
	}
}

func failureVeryDeep(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if value, err := deepFailure(300); err != nil {
			sink = 7
		} else {
			sink = value
		}
	}
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }
func httpHandler(b *testing.B) {
	handler := &Handler{}
	request := httptest.NewRequest("GET", "/", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		response := httptest.NewRecorder()
		handler.serve(response, request)
		sink = response.Code
	}
}
