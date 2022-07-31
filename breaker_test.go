package circuit_breaker

import (
	"errors"
	"github.com/benbjohnson/clock"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestCBGeneration(t *testing.T) {
	cb := NewCircuitBreaker(Options{
		Name:        "test",
		Interval:    time.Second,
		Timeout:     2 * time.Second,
		ReadyToTrip: nil,
		OnStateChange: func(name string, before, after State) {
			t.Logf("name: %s, before :%s ,after :%s", name, before, after)
		},
		IsSuccessful: nil,
		Threshold:    10,
	})
	clock := clock.New()
	set := func(state State) {
		t.Log("----")
		cb.setState(state, clock.Now())
		t.Log(cb.generation)
	}

	set(Close)
	set(Open)
	set(HalfOpen)
	set(Open)
	set(Close)

}

func TestCounts(t *testing.T) {
	c := Counts{}
	assert := func(TotalSuccess, TotalFailures, ConsecutiveSuccess, ConsecutiveFailures uint64) {
		if c.TotalSuccess != TotalSuccess {
			t.Fatalf("TotalSuccess %d -- %d", c.TotalSuccess, TotalSuccess)
			t.FailNow()
		}
		if c.TotalFailures != TotalFailures {
			t.Fatalf("TotalFailures %d -- %d", c.TotalFailures, TotalFailures)
			t.FailNow()
		}
		if c.ConsecutiveSuccess != ConsecutiveSuccess {
			t.Fatalf("ConsecutiveSuccess %d -- %d", c.ConsecutiveSuccess, ConsecutiveSuccess)
			t.FailNow()
		}
		if c.ConsecutiveFailures != ConsecutiveFailures {
			t.Fatalf("ConsecutiveFailures %d -- %d", c.ConsecutiveFailures, ConsecutiveFailures)
			t.FailNow()
		}
	}
	c.onSuccess()
	assert(1, 0, 1, 0)
	c.onSuccess()
	assert(2, 0, 2, 0)

	c.onFailure()
	assert(2, 1, 0, 1)
	c.onFailure()
	assert(2, 2, 0, 2)

	c.onSuccess()
	assert(3, 2, 1, 0)
	c.onSuccess()
	assert(4, 2, 2, 0)

	assertRequest := func(count uint64) {
		if c.TotalRequests != count {
			t.Fatalf("TotalRequests %d -- %d", c.TotalSuccess, count)
			t.FailNow()
		}
	}

	c.onRequest()
	assertRequest(1)
	c.onRequest()
	assertRequest(2)

}

func TestCb(t *testing.T) {
	timeOut := 2 * time.Second
	cb := NewCircuitBreaker(Options{
		Name:     "test",
		Interval: time.Second,
		Timeout:  timeOut,
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures >= 2
		},
		OnStateChange: func(name string, before, after State) {
			_, _, line, _ := runtime.Caller(3)
			t.Logf("name: %s, before :%s ,after :%s ,line :%d", name, before, after, line)
		},
		IsSuccessful: nil,
		Threshold:    2,
	})
	assertState := func(state State) {
		if cb.state != state {
			t.Fatalf("updateState %s - %s ", cb.state, state)
			t.FailNow()
		}
	}
	clock := clock.NewMock()

	cb.onFailure(clock.Now())
	cb.onFailure(clock.Now())
	t.Log(cb.state, cb.expiry)
	_, err := cb.beforeExecute(clock.Now())

	if err != ErrCircuitBreaker {
		t.Fatalf("updateState -> ErrCircuitBreaker :%s", err)
		t.FailNow()
	}
	clock.Add(timeOut + 1)
	_, err = cb.beforeExecute(clock.Now())
	assertState(HalfOpen)

	cb.onSuccess(clock.Now())

	cb.onFailure(clock.Now())
	assertState(Open)

	clock.Add(timeOut + 1)
	cb.updateState(clock.Now())
	assertState(HalfOpen)

	cb.onSuccess(clock.Now())
	cb.onSuccess(clock.Now())
	assertState(Close)
}

func TestCB1(t *testing.T) {
	timeOut := 2 * time.Second
	cb := NewCircuitBreaker(Options{
		Name:     "test",
		Interval: time.Second,
		Timeout:  timeOut,
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures >= 2
		},
		OnStateChange: func(name string, before, after State) {
			_, _, line, _ := runtime.Caller(3)
			t.Logf("name: %s, before :%s ,after :%s ,line :%d", name, before, after, line)
		},
		IsSuccessful: nil,
		Threshold:    2,
	})
	//assertState := func(state State) {
	//	if cb.state != state {
	//		t.Fatalf("updateState %s - %s ", cb.state, state)
	//		t.FailNow()
	//	}
	//}
	//clock := clock.NewMock()
	for i := 0; i < 3; i++ {
		fail(cb)
	}
	_, err := cb.Execute(defaultExec)
	if err != ErrCircuitBreaker {
		t.Fatalf("updateState -> ErrCircuitBreaker :%s", err)
		t.FailNow()
	}
	pseudoSleep(cb, timeOut+1)
	_, err = cb.Execute(defaultExec)
	if err != nil {
		t.Fatalf("Execute != nil :%s", err)
		t.FailNow()
	}
	fail(cb)

	_, err = cb.Execute(defaultExec)
	if err == nil {
		t.Fatalf("Execute == nil :%s", err)
		t.FailNow()
	}
	pseudoSleep(cb, timeOut+1)
	_, err = cb.Execute(defaultExec)
	_, err = cb.Execute(defaultExec)
	if cb.state != Close {
		t.Fatalf("state != close :%s", cb.state)
		t.FailNow()
	}

	fail(cb)
	fail(cb)
	pseudoSleep(cb, timeOut+1)

	go fail(cb, time.Second*10)
	go fail(cb, time.Second*10)
	time.Sleep(time.Second)
	_, err = cb.Execute(defaultExec)
	if err != ErrToManyRequests {
		t.Fatalf("err != ErrToManyRequests :%s", err)
		t.FailNow()
	}
	t.Log(err)

}
func pseudoSleep(cb *CircuitBreaker, period time.Duration) {
	if !cb.expiry.IsZero() {
		cb.expiry = cb.expiry.Add(-period)
	}
}

var defaultExec = func() (interface{}, error) {
	return nil, nil
}

func fail(cb *CircuitBreaker, sleep ...time.Duration) {
	cb.Execute(func() (interface{}, error) {
		if len(sleep) > 0 {
			time.Sleep(sleep[0])
		}
		return nil, errors.New("test")
	})
}

func TestParallelize(t *testing.T) {

	cb := NewCircuitBreaker(Options{
		Name:     "test",
		Interval: time.Second,
		Timeout:  time.Second * 2,
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures >= 2
		},
		OnStateChange: func(name string, before, after State) {
			_, _, line, _ := runtime.Caller(3)
			t.Logf("name: %s, before :%s ,after :%s ,line :%d", name, before, after, line)
		},
		IsSuccessful: nil,
		Threshold:    2,
	})
	wg := sync.WaitGroup{}
	fail(cb)
	fail(cb)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cb.Execute(func() (interface{}, error) {
				return nil, nil
			})
			if err != nil {
				t.Log(err)
			}
		}()
	}
	wg.Wait()
	pseudoSleep(cb, time.Second*2+1)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cb.Execute(func() (interface{}, error) {
				return nil, nil
			})
			if err != nil {
				t.Fatalf("Execute err not nil :%s", err)
				t.FailNow()
			}
		}()
	}
	wg.Wait()
}

func TestTwoStep(t *testing.T) {
	cb := NewTwoStepCircuitBreaker(Options{
		Name:     "test",
		Interval: time.Second,
		Timeout:  time.Second * 2,
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures >= 2
		},
		OnStateChange: func(name string, before, after State) {
			_, _, line, _ := runtime.Caller(3)
			t.Logf("name: %s, before :%s ,after :%s ,line :%d", name, before, after, line)
		},
		IsSuccessful: nil,
		Threshold:    2,
	})
	done, err := cb.IsAllow()
	if err != nil {
		return
	}
	//do something
	done(true)

}
