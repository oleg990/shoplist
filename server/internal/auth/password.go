package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Параметры argon2id (рекомендация OWASP: не менее 19 МиБ памяти при t=2; берём с запасом).
const (
	argonTime    = 2
	argonMemory  = 64 * 1024 // КиБ
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16
)

// hashPassword возвращает строку вида $argon2id$v=19$m=65536,t=2,p=2$<соль>$<хэш>.
func hashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

var errBadHash = errors.New("malformed password hash")

// verifyPassword сравнивает пароль с хэшем за постоянное время; параметры берутся из самой строки.
func verifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errBadHash
	}
	var mem, t uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &threads); err != nil {
		return false, errBadHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errBadHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, errBadHash
	}
	got := argon2.IDKey([]byte(password), salt, t, mem, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash тратит столько же времени, сколько настоящая проверка: так по скорости ответа
// нельзя отличить несуществующий логин от неверного пароля.
var dummyHash = func() string {
	h, err := hashPassword("dummy-password-for-timing")
	if err != nil {
		panic(err)
	}
	return h
}()
