package httpapi

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestRenderCapacity(t *testing.T) {
	s := &Server{renderSlots: make(chan struct{}, 2)}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	handler := s.withRenderSlot(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-release; w.WriteHeader(200) })
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handler(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/charts", nil))
		}()
	}
	<-entered
	<-entered
	recorder := httptest.NewRecorder()
	handler(recorder, httptest.NewRequest("POST", "/v1/charts", nil))
	if recorder.Code != 429 || recorder.Header().Get("Retry-After") == "" {
		t.Fatal("renderer capacity not bounded")
	}
	close(release)
	wg.Wait()
	if len(s.renderSlots) != 0 {
		t.Fatal("render slots were not released")
	}
}
