package model

// JobStatus represents the lifecycle state of a media job.
type JobStatus string

const (
	StatusCreated    JobStatus = "created"
	StatusQueued     JobStatus = "queued"
	StatusInProgress JobStatus = "in_progress"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
)

// JobStage represents the active processing stage.
type JobStage string

const (
	StageDownload JobStage = "download"
	StageConvert  JobStage = "convert"
	StageTransfer JobStage = "transfer"
)

// JobPriority represents processing priority.
type JobPriority string

const (
	PriorityLow    JobPriority = "low"
	PriorityNormal JobPriority = "normal"
	PriorityHigh   JobPriority = "high"
)
