package scheduler

import (
	"testing"
	"time"

	"github.com/christopherklint97/clockr/internal/config"
)

func TestIsWorkTime_Weekday_InHours(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			WorkStart: "09:00",
			WorkEnd:   "17:00",
			WorkDays:  []int{1, 2, 3, 4, 5},
		},
	}
	// Wednesday 2026-03-04 12:00
	wed := time.Date(2026, 3, 4, 12, 0, 0, 0, time.Local)
	if !IsWorkTime(cfg, wed) {
		t.Error("expected Wednesday 12:00 to be work time")
	}
}

func TestIsWorkTime_Weekday_BeforeHours(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			WorkStart: "09:00",
			WorkEnd:   "17:00",
			WorkDays:  []int{1, 2, 3, 4, 5},
		},
	}
	// Wednesday 2026-03-04 07:00
	wed := time.Date(2026, 3, 4, 7, 0, 0, 0, time.Local)
	if IsWorkTime(cfg, wed) {
		t.Error("expected Wednesday 07:00 to not be work time")
	}
}

func TestIsWorkTime_Weekday_AfterHours(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			WorkStart: "09:00",
			WorkEnd:   "17:00",
			WorkDays:  []int{1, 2, 3, 4, 5},
		},
	}
	// Wednesday 2026-03-04 18:00
	wed := time.Date(2026, 3, 4, 18, 0, 0, 0, time.Local)
	if IsWorkTime(cfg, wed) {
		t.Error("expected Wednesday 18:00 to not be work time")
	}
}

func TestIsWorkTime_Weekend(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			WorkStart: "09:00",
			WorkEnd:   "17:00",
			WorkDays:  []int{1, 2, 3, 4, 5},
		},
	}
	// Saturday 2026-03-07 12:00
	sat := time.Date(2026, 3, 7, 12, 0, 0, 0, time.Local)
	if IsWorkTime(cfg, sat) {
		t.Error("expected Saturday to not be work time")
	}
}

func TestIsWorkTime_Sunday(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			WorkStart: "09:00",
			WorkEnd:   "17:00",
			WorkDays:  []int{1, 2, 3, 4, 5},
		},
	}
	// Sunday 2026-03-08 12:00
	sun := time.Date(2026, 3, 8, 12, 0, 0, 0, time.Local)
	if IsWorkTime(cfg, sun) {
		t.Error("expected Sunday to not be work time")
	}
}

func TestIsWorkTime_AtStartBoundary(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			WorkStart: "09:00",
			WorkEnd:   "17:00",
			WorkDays:  []int{1, 2, 3, 4, 5},
		},
	}
	// Wednesday 2026-03-04 09:00 exactly
	wed := time.Date(2026, 3, 4, 9, 0, 0, 0, time.Local)
	if !IsWorkTime(cfg, wed) {
		t.Error("expected Wednesday 09:00 to be work time (inclusive start)")
	}
}

func TestIsWorkTime_AtEndBoundary(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			WorkStart: "09:00",
			WorkEnd:   "17:00",
			WorkDays:  []int{1, 2, 3, 4, 5},
		},
	}
	// Wednesday 2026-03-04 17:00 exactly
	wed := time.Date(2026, 3, 4, 17, 0, 0, 0, time.Local)
	if !IsWorkTime(cfg, wed) {
		t.Error("expected Wednesday 17:00 to be work time (inclusive end)")
	}
}

func TestIsWorkTime_CustomWorkDays(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			WorkStart: "09:00",
			WorkEnd:   "17:00",
			WorkDays:  []int{6, 7}, // Saturday and Sunday only
		},
	}
	// Saturday 2026-03-07 12:00
	sat := time.Date(2026, 3, 7, 12, 0, 0, 0, time.Local)
	if !IsWorkTime(cfg, sat) {
		t.Error("expected Saturday to be work time with custom work days")
	}
	// Wednesday 2026-03-04 12:00
	wed := time.Date(2026, 3, 4, 12, 0, 0, 0, time.Local)
	if IsWorkTime(cfg, wed) {
		t.Error("expected Wednesday to not be work time with weekend-only config")
	}
}

func TestSetSkipWorkTimeCheck(t *testing.T) {
	cfg := &config.Config{
		Schedule: config.ScheduleConfig{
			IntervalMinutes: 60,
			WorkStart:       "09:00",
			WorkEnd:         "17:00",
			WorkDays:        []int{1, 2, 3, 4, 5},
		},
	}
	sched := New(cfg, nil, nil, nil, "ws-123")
	if sched.skipWorkTimeCheck {
		t.Error("expected skipWorkTimeCheck to be false by default")
	}
	sched.SetSkipWorkTimeCheck(true)
	if !sched.skipWorkTimeCheck {
		t.Error("expected skipWorkTimeCheck to be true after setting")
	}
	sched.SetSkipWorkTimeCheck(false)
	if sched.skipWorkTimeCheck {
		t.Error("expected skipWorkTimeCheck to be false after unsetting")
	}
}
