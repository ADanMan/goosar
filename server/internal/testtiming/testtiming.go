// Пакет testtiming масштабирует те немногие тестовые дедлайны, которые
// ограничивают реальный подпроцесс или syscall и не могут быть герметичными.
package testtiming

import (
	"os"
	"strconv"
	"time"
)

var Factor = factorFromEnv()

func factorFromEnv() float64 {
	f := float64(raceFactor)
	if raw := os.Getenv("GOOSAR_TEST_TIME_SCALE"); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil && parsed > 0 {
			f *= parsed
		}
	}
	return f
}

func Scale(d time.Duration) time.Duration {
	scaled := time.Duration(float64(d) * Factor)
	if scaled < d {
		return d
	}
	return scaled
}
