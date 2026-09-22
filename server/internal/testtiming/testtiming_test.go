package testtiming

import (
	"testing"
	"time"
)

func TestScaleNeverShrinksABudget(t *testing.T) {
	if got := Scale(time.Second); got < time.Second {
		t.Fatalf("Scale(1s) = %s, want at least 1s", got)
	}
	if got := Scale(0); got != 0 {
		t.Fatalf("Scale(0) = %s, want 0", got)
	}
}

func TestFactorFromEnvMultipliesTheRaceFactor(t *testing.T) {
	t.Setenv("GOOSAR_TEST_TIME_SCALE", "2.5")
	if got, want := factorFromEnv(), float64(raceFactor)*2.5; got != want {
		t.Fatalf("factorFromEnv() = %v, want %v", got, want)
	}

	t.Setenv("GOOSAR_TEST_TIME_SCALE", "0")
	if got, want := factorFromEnv(), float64(raceFactor); got != want {
		t.Fatalf("factorFromEnv() with GOOSAR_TEST_TIME_SCALE=0 = %v, want %v", got, want)
	}
	t.Setenv("GOOSAR_TEST_TIME_SCALE", "nonsense")
	if got, want := factorFromEnv(), float64(raceFactor); got != want {
		t.Fatalf("factorFromEnv() with garbage = %v, want %v", got, want)
	}
}
