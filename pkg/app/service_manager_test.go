package app

import (
	"fmt"
	"io"
	"testing"

	"github.com/sirupsen/logrus"
)

func testLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}

// fakeService records start/stop invocations into a shared event log.
type fakeService struct {
	name     string
	events   *[]string
	startErr error
	stopErr  error
}

func (s *fakeService) Start() error {
	*s.events = append(*s.events, "start:"+s.name)
	return s.startErr
}

func (s *fakeService) Stop() error {
	*s.events = append(*s.events, "stop:"+s.name)
	return s.stopErr
}

func TestServiceManager_StartAll_InRegistrationOrder(t *testing.T) {
	sm := NewServiceManager(testLogger())
	var events []string

	sm.Register("first", &fakeService{name: "first", events: &events})
	sm.Register("second", &fakeService{name: "second", events: &events})
	sm.Register("third", &fakeService{name: "third", events: &events})

	if err := sm.StartAll(); err != nil {
		t.Fatalf("StartAll() error: %v", err)
	}

	want := []string{"start:first", "start:second", "start:third"}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Errorf("expected start order %v, got %v", want, events)
	}
}

func TestServiceManager_StopAll_InReverseOrder(t *testing.T) {
	sm := NewServiceManager(testLogger())
	var events []string

	sm.Register("first", &fakeService{name: "first", events: &events})
	sm.Register("second", &fakeService{name: "second", events: &events})

	if err := sm.StopAll(); err != nil {
		t.Fatalf("StopAll() error: %v", err)
	}

	want := []string{"stop:second", "stop:first"}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Errorf("expected stop order %v, got %v", want, events)
	}
}

func TestServiceManager_StartAll_StopsOnError(t *testing.T) {
	sm := NewServiceManager(testLogger())
	var events []string

	sm.Register("ok", &fakeService{name: "ok", events: &events})
	sm.Register("broken", &fakeService{name: "broken", events: &events, startErr: fmt.Errorf("boom")})
	sm.Register("never", &fakeService{name: "never", events: &events})

	err := sm.StartAll()
	if err == nil {
		t.Fatal("expected error from failing service")
	}

	for _, event := range events {
		if event == "start:never" {
			t.Error("expected services after the failing one to not be started")
		}
	}
}

func TestServiceManager_StopAll_ContinuesOnError(t *testing.T) {
	sm := NewServiceManager(testLogger())
	var events []string

	sm.Register("first", &fakeService{name: "first", events: &events})
	sm.Register("failing", &fakeService{name: "failing", events: &events, stopErr: fmt.Errorf("boom")})

	if err := sm.StopAll(); err != nil {
		t.Fatalf("StopAll() should not propagate stop errors, got: %v", err)
	}

	want := []string{"stop:failing", "stop:first"}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Errorf("expected all services stopped despite error, got %v", events)
	}
}

func TestServiceManager_Get(t *testing.T) {
	sm := NewServiceManager(testLogger())
	var events []string
	service := &fakeService{name: "svc", events: &events}

	sm.Register("svc", service)

	if got := sm.Get("svc"); got != service {
		t.Error("expected registered service to be returned")
	}
	if got := sm.Get("missing"); got != nil {
		t.Error("expected nil for unregistered service")
	}
}

func TestServiceManager_TypedGetters_NilWhenMissing(t *testing.T) {
	sm := NewServiceManager(testLogger())

	if sm.GetMQTTClient() != nil {
		t.Error("expected nil MQTT client when not registered")
	}
	if sm.GetHomeAssistantIntegration() != nil {
		t.Error("expected nil integration when not registered")
	}
	if sm.GetScannerManager() != nil {
		t.Error("expected nil scanner manager when not registered")
	}
}
