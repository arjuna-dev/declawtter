package scheduler

import "time"

type ScheduleConfig struct {
	Kind          string `json:"kind"`
	Year          int    `json:"year,omitempty"`
	Month         int    `json:"month,omitempty"`
	Day           int    `json:"day,omitempty"`
	Hour          int    `json:"hour"`
	Minute        int    `json:"minute"`
	Weekdays      []int  `json:"weekdays,omitempty"`
	ScheduledTime string `json:"scheduled_time,omitempty"`
}

type JobRecord struct {
	Name               string         `json:"name"`
	Type               string         `json:"type"`
	CreatedAt          time.Time      `json:"created_at"`
	Config             ScheduleConfig `json:"config"`
	Prompt             string         `json:"prompt,omitempty"`
	Workspace          string         `json:"workspace,omitempty"`
	Title              string         `json:"title,omitempty"`
	Body               string         `json:"body,omitempty"`
	Cwd                string         `json:"cwd,omitempty"`
	Stdout             string         `json:"stdout,omitempty"`
	Stderr             string         `json:"stderr,omitempty"`
	Env                []string       `json:"env,omitempty"`
	HasRecovery        bool           `json:"has_recovery"`
	PrimaryLabel       string         `json:"primary_label"`
	RecoveryLabel      string         `json:"recovery_label,omitempty"`
	OnceLabel          string         `json:"once_label,omitempty"`
	WorkspaceBootstrap bool           `json:"workspace_bootstrap"`
}
