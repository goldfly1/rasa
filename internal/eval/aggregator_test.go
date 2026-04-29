package eval

import (
	"testing"
)

func TestDriftWindowPush(t *testing.T) {
	w := NewDriftWindow(5)

	w.Push("coder", 1.0)
	w.Push("coder", 0.8)
	w.Push("coder", 1.0)

	n, avg := w.Stats("coder")
	if n != 3 {
		t.Errorf("count: want 3, got %d", n)
	}
	if avg < 0.9 || avg > 0.95 {
		t.Errorf("avg: want ~0.93, got %.2f", avg)
	}
}

func TestDriftWindowNoAlertWithFewSamples(t *testing.T) {
	w := NewDriftWindow(5)
	w.Push("coder", 0.0)
	w.Push("coder", 0.0)

	if w.ShouldAlert("coder", 0.6) {
		t.Error("should not alert with only 2 of 5 required samples")
	}
}

func TestDriftWindowAlert(t *testing.T) {
	w := NewDriftWindow(3)
	w.Push("coder", 0.3)
	w.Push("coder", 0.2)
	w.Push("coder", 0.1)

	if !w.ShouldAlert("coder", 0.6) {
		t.Error("should alert: avg=0.2 below 0.6 threshold")
	}
}

func TestDriftWindowNoAlertWhenHealthy(t *testing.T) {
	w := NewDriftWindow(3)
	w.Push("coder", 0.9)
	w.Push("coder", 0.8)
	w.Push("coder", 0.9)

	if w.ShouldAlert("coder", 0.6) {
		t.Error("should not alert: avg=0.87 above 0.6 threshold")
	}
}

func TestDriftWindowOverflow(t *testing.T) {
	w := NewDriftWindow(3)
	w.Push("coder", 1.0)
	w.Push("coder", 1.0)
	w.Push("coder", 1.0)
	w.Push("coder", 0.0)
	w.Push("coder", 0.0)

	n, _ := w.Stats("coder")
	if n != 3 {
		t.Errorf("overflow count: want 3, got %d", n)
	}
}

func TestDriftWindowStatsEmpty(t *testing.T) {
	w := NewDriftWindow(5)
	n, avg := w.Stats("nonexistent")
	if n != 0 {
		t.Errorf("empty count: want 0, got %d", n)
	}
	if avg != 0 {
		t.Errorf("empty avg: want 0, got %.2f", avg)
	}
}
