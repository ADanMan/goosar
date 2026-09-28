// crypto.go — запечатывание секрета подписи вебхука (strig_signing_secret_sealed,
// contract: setAutopilotTriggerSigningSecret). До доводки T-029 этот файл
// реализовывал AES-256-GCM сам (независимо от internal/agent/internal/seal);
// доводка сводит все три к internal/seal — sealSecret/openSecret здесь тонкие
// обёртки над seal.Seal/seal.Open. internal/seal читает и байты, написанные
// этим файлом до доводки (тот же алгоритм, без маркера — см. internal/seal,
// "исторический формат"), так что перевод не требует миграции уже
// сохранённых секретов.
package autopilot

import (
	"github.com/adanman/goosar/server2/internal/seal"
)

// ErrEncryptionUnavailable — GOOSAR_MCP_SECRET_KEY не задан: сохранить
// секрет подписи невозможно (contract: тот же 503, что и MFA/секреты
// воркспейса, см. docs/50-api-contract.md §1.9 "MFA").
var ErrEncryptionUnavailable = seal.ErrUnavailable

func sealSecret(key, plaintext string) ([]byte, error) {
	return seal.Seal(key, []byte(plaintext))
}

// openSecret расшифровывает sealed, пробуя key, затем prevKey (ротация
// GOOSAR_MCP_SECRET_KEY_PREVIOUS).
func openSecret(key, prevKey string, sealed []byte) (string, error) {
	pt, ok := seal.Open(key, prevKey, sealed)
	if !ok {
		return "", ErrEncryptionUnavailable
	}
	return string(pt), nil
}
