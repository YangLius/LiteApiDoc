// Package auth 实现双密码模型:平台管理密码与项目访问密码的
// bcrypt 哈希校验、HMAC 签名 token 签发/校验,以及内存防爆破锁定。
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Token kinds
const (
	KindAdmin  = "admin"
	KindProj   = "project"
	KindShare  = "share"
	TokenTTL   = 7 * 24 * time.Hour // 免登录体验:token 7 天有效
)

// ErrLocked 防爆破锁定中。
type LockedError struct{ RetryAfter time.Duration }

func (e *LockedError) Error() string {
	return fmt.Sprintf("尝试次数过多,请 %d 分钟后再试", int(e.RetryAfter.Minutes()+0.5))
}

// HashPassword bcrypt 加盐哈希。
func HashPassword(pwd string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword 校验密码。
func CheckPassword(hash, pwd string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pwd)) == nil
}

// GenSecret 生成随机密钥(HMAC 签名用)。
func GenSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Manager token 签发与校验。
type Manager struct {
	secret []byte
}

func NewManager(secretHex string) *Manager {
	s, err := hex.DecodeString(secretHex)
	if err != nil || len(s) == 0 {
		s = []byte(secretHex)
	}
	return &Manager{secret: s}
}

// Sign 签发 token:base64(kind:id:expiry:hmac)。
func (m *Manager) Sign(kind string, id int64) string {
	exp := time.Now().Add(TokenTTL).Unix()
	payload := fmt.Sprintf("%s:%d:%d", kind, id, exp)
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + sig))
}

// Verify 校验 token,返回关联 ID。kind=KindAdmin 时 id 恒为 0。
func (m *Manager) Verify(token, kind string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, errors.New("invalid token")
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 4 || parts[0] != kind {
		return 0, errors.New("invalid token")
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || exp < time.Now().Unix() {
		return 0, errors.New("token expired")
	}
	payload := strings.Join(parts[:3], ":")
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(payload))
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(parts[3])) {
		return 0, errors.New("bad signature")
	}
	id, _ := strconv.ParseInt(parts[1], 10, 64)
	return id, nil
}

// Lockout 内存防爆破:同一 key 连续失败 5 次锁定 10 分钟。
type Lockout struct {
	mu     sync.Mutex
	fails  map[string]*attempt
}

type attempt struct {
	count       int
	lockedUntil time.Time
}

const (
	maxFails     = 5
	lockDuration = 10 * time.Minute
)

func NewLockout() *Lockout {
	return &Lockout{fails: map[string]*attempt{}}
}

// Check 返回 nil 表示允许尝试。
func (l *Lockout) Check(key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.fails[key]
	if !ok {
		return nil
	}
	if remain := time.Until(a.lockedUntil); remain > 0 {
		return &LockedError{RetryAfter: remain}
	}
	return nil
}

// Fail 记录一次失败,达到阈值即锁定。
func (l *Lockout) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.fails[key]
	if !ok || time.Now().After(a.lockedUntil) && a.lockedUntil.IsZero() == false {
		// 锁定过期后重新计数
		if ok && time.Now().After(a.lockedUntil) {
			a = &attempt{}
			l.fails[key] = a
		}
	}
	if !ok {
		a = &attempt{}
		l.fails[key] = a
	}
	a.count++
	if a.count >= maxFails {
		a.lockedUntil = time.Now().Add(lockDuration)
		a.count = 0
	}
}

// Reset 校验成功后清零。
func (l *Lockout) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}
