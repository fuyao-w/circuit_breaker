# 熔断器

##用法：
```go
    cb := NewCircuitBreaker(Options{
		Name:     "http",
		Interval: time.Second,
		Timeout:  timeOut,
		ReadyToTrip: func(counts Counts) bool {
			return counts.ConsecutiveFailures >= 2
		},
		OnStateChange: func(name string, before, after State) {
			t.Logf("name: %s, before :%s ,after :%s ,line :%d", name, before, after, line)
		},
		IsSuccessful: nil,
		Threshold:    2,
	})
    resp,err := cb.Execute(func() (interface{}, error) {
        // do something
        return nil, nil
    })
	if err == ErrCircuitBreaker {
	    // reject or downgrade strategy
    }

```

```go
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

```