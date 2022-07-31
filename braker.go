package circuit_breaker

import (
	"errors"
	"sync"
	"time"
)

/*
	熔断器实现
	状态： 1. 关闭 2.开启 3.半开启

	状态转移以及条件：
	关闭 -> 开启 ： 初始状态：关闭 ，当出现失败的计数达到 ReadyToTrip 条件的时候，转为开启态，开启态不会接受新请求
	开启 -> 半开启：当转为开启态一段时间后 [timeOut 控制] ，状态转换为半开启态。此时可以尝试接受请求，但是接受的请求次数不能超过 threshold，超过会被拒绝
	半开启 -> 开启：当处于半开启状态时，开始尝试接受请求，如果出现一例失败的请求则重新回到开启态，拒绝接受请求直到开启态再次达到一定时间后又会转为半开启
	半开启 -> 关闭：当处于半开启状态时，开始尝试接受请求，如果连续成功的请求次数达到阈值，则转到关闭态，此时可以正常接收请求


*/
type State int // 熔断器状态

func (s State) String() string {
	switch s {
	case Close:
		return "Close"
	case Open:
		return "Open"
	case HalfOpen:
		return "HalfOpen"
	}
	return "Unknown"
}

const (
	Close    State = iota //关闭
	Open                  //熔断开启
	HalfOpen              //熔断半开启
)
const (
	defaultInterval = 1 * time.Second  // 默认的循环间隔
	defaultTimeout  = 60 * time.Second // 默认的熔断超时时间
)

var (
	ErrCircuitBreaker = errors.New("circuit breaker")
	ErrToManyRequests = errors.New("too many requests")

	defaultReadyToTrip = func(counts Counts) bool {
		return counts.ConsecutiveFailures >= 10
	}
	defaultIsSuccessful = func(err error) bool {
		return err == nil
	}
)

type Options struct {
	Name          string                                 // 熔断器名称
	Interval      time.Duration                          // 循环间隔，每个时间间隔会重新统计一次计数信息
	Timeout       time.Duration                          // 熔断超时时间
	ReadyToTrip   func(counts Counts) bool               // 是否开启熔断
	OnStateChange func(name string, before, after State) // 状态切换回调
	IsSuccessful  func(err error) bool                   // 返回的错误是否代表成功处理
	Threshold     uint64                                 // 熔断半开启 -> 关闭的请求成功数阈值，并且在半开启状态，请求数不能超过该阈值
}

// TwoStepCircuitBreaker 该熔断器不会接受处理函数，调用者使用 IsAllow 函数获取当前状态，自己执行完业务逻辑后，根据执行结果通知给回调函数变更状态
type TwoStepCircuitBreaker struct {
	cb *CircuitBreaker
}

// CircuitBreaker 普通熔断器，接受一个处理函数，并根据当前状态判断是否真正调用处理函数
type CircuitBreaker struct {
	opt        *Options
	counts     *Counts   // 计数信息
	generation uint64    // 循环的代数
	expiry     time.Time // 当前周期的过期时间
	state      State
	mu         *sync.Mutex
}

type Counts struct {
	TotalRequests       uint64
	TotalSuccess        uint64
	TotalFailures       uint64
	ConsecutiveSuccess  uint64
	ConsecutiveFailures uint64
}

func NewCircuitBreaker(opt Options) (cb *CircuitBreaker) {

	if opt.Interval <= 0 {
		opt.Interval = defaultInterval
	}
	if opt.Timeout <= 0 {
		opt.Timeout = defaultTimeout
	}
	if opt.Threshold == 0 {
		opt.Threshold = 1
	}
	if opt.ReadyToTrip == nil {
		opt.ReadyToTrip = defaultReadyToTrip
	}
	if opt.IsSuccessful == nil {
		opt.IsSuccessful = defaultIsSuccessful
	}

	cb = &CircuitBreaker{
		opt:        &opt,
		counts:     new(Counts),
		generation: 0,
		expiry:     time.Time{},
		state:      Close,
		mu:         new(sync.Mutex),
	}
	cb.newGeneration(time.Now())
	return
}
func NewTwoStepCircuitBreaker(opt Options) (cb *TwoStepCircuitBreaker) {
	return &TwoStepCircuitBreaker{
		cb: NewCircuitBreaker(opt),
	}
}

func (c *CircuitBreaker) Counts() Counts {
	c.mu.Lock()
	c.mu.Unlock()
	return *c.counts
}

func (c *TwoStepCircuitBreaker) Counts() Counts {
	return c.cb.Counts()
}

// IsAllow
func (c *TwoStepCircuitBreaker) IsAllow() (func(success bool), error) {
	beforeGeneration, err := c.cb.beforeExecute(time.Now())
	if err != nil {
		return nil, err
	}
	return func(success bool) {
		c.cb.afterExecute(beforeGeneration, success, time.Now())
	}, nil

}

// Execute 执行业务逻辑，如果熔断开启 error 返回 ErrCircuitBreaker,如果熔断半半开启并且请求次数超过半开启阈值返回 ErrToManyRequests
func (c *CircuitBreaker) Execute(req func() (interface{}, error)) (interface{}, error) {
	beforeGeneration, err := c.beforeExecute(time.Now())
	if err != nil {
		return nil, err
	}
	defer func() {
		if p := recover(); p != nil {
			c.afterExecute(beforeGeneration, c.opt.IsSuccessful(err), time.Now())
			panic(p)
		}
	}()
	resp, err := req()
	c.afterExecute(beforeGeneration, c.opt.IsSuccessful(err), time.Now())
	return resp, err
}

func (c *Counts) clear() {
	c.TotalRequests = 0
	c.TotalSuccess = 0
	c.TotalFailures = 0
	c.ConsecutiveSuccess = 0
	c.ConsecutiveFailures = 0
}

func (c *Counts) onRequest() {
	c.TotalRequests++
}
func (c *Counts) onSuccess() {
	c.TotalSuccess++
	c.ConsecutiveSuccess++
	c.ConsecutiveFailures = 0
}

func (c *Counts) onFailure() {
	c.TotalFailures++
	c.ConsecutiveFailures++
	c.ConsecutiveSuccess = 0
}

// setState 设置当前状态、更新循环代数
func (c *CircuitBreaker) setState(newState State, now time.Time) {
	if c.state == newState {
		return
	}
	oldState := c.state
	c.state = newState

	c.newGeneration(now)

	if c.opt.OnStateChange != nil {
		c.opt.OnStateChange(c.opt.Name, oldState, newState)
	}
}

// updateState 更新状态、分代。半开启状态不用更新，关闭状态下会计算新循环代数，开启状态下会计算新代数，并更新状态
func (c *CircuitBreaker) updateState(now time.Time) {
	switch c.state {
	case Close:
		if !c.expiry.IsZero() && c.expiry.Before(now) {
			c.newGeneration(now)
		}
	case Open:
		if c.expiry.Before(now) {
			c.newGeneration(now)
			c.setState(HalfOpen, now)
		}
	case HalfOpen:
	}
}

// newGeneration 进入新一代循环，之前的计数降被清空，并且重新计算下一循环的超时时间
func (c *CircuitBreaker) newGeneration(now time.Time) uint64 {
	c.generation++
	c.counts.clear()
	var zero time.Time
	switch c.state {
	case Close:
		if c.opt.Interval <= 0 {
			c.expiry = zero
		} else {
			c.expiry = now.Add(c.opt.Interval)
		}
	case Open:
		c.expiry = now.Add(c.opt.Timeout)
	case HalfOpen:
		c.expiry = zero
	}
	return c.generation
}

// onSuccess 成功情况下的处理，会增加成功计数，并根据当前状态计算下一代或者切换状态
func (c *CircuitBreaker) onSuccess(now time.Time) {
	c.counts.onSuccess()
	switch c.state {
	case Close:
		if c.expiry.Before(now) {
			c.newGeneration(now)
		}
	case HalfOpen:
		if c.counts.ConsecutiveSuccess >= c.opt.Threshold {
			c.setState(Close, now)
		}
	}
}

// onFailure 失败处理，会根据当前状态切换到新状态
func (c *CircuitBreaker) onFailure(now time.Time) {
	switch c.state {
	case Close:
		c.counts.onFailure()
		if c.opt.ReadyToTrip(*c.counts) {
			c.setState(Open, now)
		}
	case HalfOpen:
		c.setState(Open, now)
	}
}

// beforeExecute 执行前判断，首先更新状态和分代，然后根据最新状态判断是否失败，如果不失败返回当前分代
func (c *CircuitBreaker) beforeExecute(now time.Time) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.updateState(now)

	switch c.state {
	case Open:
		return c.generation, ErrCircuitBreaker
	case HalfOpen:
		if c.counts.TotalRequests >= c.opt.Threshold {
			return c.generation, ErrToManyRequests
		}
	}
	c.counts.onRequest()
	return c.generation, nil
}

// afterExecute 执行后计算分代，并且根据执行结果更新状态
func (c *CircuitBreaker) afterExecute(beforeGeneration uint64, success bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.updateState(now)
	if beforeGeneration != c.generation {
		return
	}
	switch success {
	case true:
		c.onSuccess(now)
	default:
		c.onFailure(now)
	}
}
