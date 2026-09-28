package agent

// Шифрование секретных JSON-полей агента (runtime_config/mcp_config/custom_env,
// колонки op_*_sealed в 003_agents.up.sql). Контракт (docs/50-api-contract.md
// §1.9, "MFA") прямо называет механизм: "GOOSAR_MCP_SECRET_KEY (тот же ключ
// шифрует и MCP-конфиги агентов, и TOTP-секреты; без него — ... сохранённые
// MCP-конфиги пишутся в БД plaintext), GOOSAR_MCP_SECRET_KEY_PREVIOUS (ротация
// ключа)". До доводки T-029 этот файл реализовывал AES-256-GCM+маркер сам, не
// делясь кодом с internal/autopilot/internal/seal — три независимые копии
// одного алгоритма с чуть разными форматами. Доводка T-029 сводит все три к
// internal/seal: sealJSON/openJSON здесь — тонкие обёртки над
// seal.SealJSONOptional/seal.OpenJSONOptional (единый формат, включая маркер
// и чтение старого/нового формата — см. internal/seal, пакет умеет открыть и
// то, что этот файл писал до доводки, байт-в-байт совместимо, так что
// перевод не требует миграции уже сохранённых данных).
import (
	"github.com/adanman/goosar/server2/internal/seal"
)

// sealJSON сериализует v и, если key задан, шифрует результат; при пустом key
// возвращает JSON как есть (contract: "без ключа — plaintext"). encrypted
// сообщает, что фактически произошло — для op_mcp_config_encrypted и
// подобных полей ответа.
func sealJSON(key string, v any) (sealed []byte, encrypted bool, err error) {
	return seal.SealJSONOptional(key, v)
}

// openJSON — обратная операция: расшифровывает sealed (пробуя key, затем
// prevKey при ротации, contract GOOSAR_MCP_SECRET_KEY_PREVIOUS) и разбирает
// JSON в out. ok=false — значение зашифровано, но ни один из ключей не
// подошёл (mcp_config_redacted у вызывающего кода).
func openJSON(key, prevKey string, sealedVal []byte, out any) (ok bool) {
	_, ok = seal.OpenJSONOptional(key, prevKey, sealedVal, out)
	return ok
}
