package httpapi

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// limiter: ведро токенов на каждый ключ (IP). Хранится в памяти одного процесса.
type limiter struct {
	mu      sync.Mutex
	rate    float64 // токенов в секунду
	burst   float64
	now     func() time.Time
	buckets map[string]*bucket
	sweepAt time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(perSecond float64, burst int) *limiter {
	return &limiter{rate: perSecond, burst: float64(burst), now: time.Now, buckets: map[string]*bucket{}}
}

// allow забирает токен; если ведро пусто, возвращает, через сколько секунд можно повторить.
func (l *limiter) allow(key string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, int(math.Ceil((1 - b.tokens) / l.rate))
}

// sweep раз в минуту выбрасывает ключи, у которых ведро снова полное: так память не растёт.
func (l *limiter) sweep(now time.Time) {
	if now.Before(l.sweepAt) {
		return
	}
	l.sweepAt = now.Add(time.Minute)
	for k, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*l.rate >= l.burst {
			delete(l.buckets, k)
		}
	}
}

// limit — middleware: не больше perSecond запросов в секунду (с запасом burst) с одного IP.
func limit(perSecond float64, burst int) func(http.Handler) http.Handler {
	l := newLimiter(perSecond, burst)
	return limitWith(l)
}

func limitWith(l *limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// middleware.RealIP уже положил адрес клиента в RemoteAddr.
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			if ok, retry := l.allow(ip); !ok {
				w.Header().Set("Retry-After", strconv.Itoa(retry))
				writeError(w, http.StatusTooManyRequests, "too many requests, slow down")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
