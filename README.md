# 熔断器

[![Security Status](https://www.murphysec.com/platform3/v3/badge/1611986234155499520.svg)](https://www.murphysec.com/accept?code=fd11cf7ded39bd1da51afdd0ae7a913c&type=1&from=2&t=2)

## 用法：

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
