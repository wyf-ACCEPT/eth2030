// pipeline.go implements the SyncPipeline, a stage-based orchestrator for
// full sync: header download -> body download -> receipt download ->
// state download -> verification.
package sync

import (
	"errors"
	"fmt"
	gosync "sync"
)

// Pipeline-specific errors.
var (
	ErrPipelineStageNotFound  = errors.New("pipeline: stage not found")
	ErrPipelineStageDeps      = errors.New("pipeline: stage dependencies not met")
	ErrPipelineStageActive    = errors.New("pipeline: stage is already active or completed")
	ErrPipelineRetryExhausted = errors.New("pipeline: retry limit exhausted for stage")
)

// StageStatus represents the lifecycle state of a pipeline stage.
type StageStatus uint8

const (
	StageStatusPending   StageStatus = iota // Waiting to start.
	StageStatusRunning                      // Currently executing.
	StageStatusCompleted                    // Finished successfully.
	StageStatusFailed                       // Failed (may be retried).
)

// String returns a human-readable name for the status.
func (s StageStatus) String() string {
	switch s {
	case StageStatusPending:
		return "pending"
	case StageStatusRunning:
		return "running"
	case StageStatusCompleted:
		return "completed"
	case StageStatusFailed:
		return "failed"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

// Well-known pipeline stage names.
const (
	PipelineStageHeaderSync   = "HeaderSync"
	PipelineStageBodySync     = "BodySync"
	PipelineStageReceiptSync  = "ReceiptSync"
	PipelineStageStateSync    = "StateSync"
	PipelineStageVerification = "Verification"
)

// PipelineConfig configures the sync pipeline behavior.
type PipelineConfig struct {
	// MaxConcurrentStages limits how many stages can run simultaneously.
	// Zero means no limit.
	MaxConcurrentStages int

	// StageTimeout is the timeout in seconds for each stage. Zero means
	// no timeout (informational only; enforced by caller).
	StageTimeout int

	// RetryLimit is the maximum number of times a failed stage can be
	// retried. Zero means no retries allowed.
	RetryLimit int
}

// DefaultPipelineConfig returns a PipelineConfig with sensible defaults.
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		MaxConcurrentStages: 1,
		StageTimeout:        60,
		RetryLimit:          3,
	}
}

// PipelineStage represents a single stage in the sync pipeline.
type PipelineStage struct {
	// Name is the unique name identifying this stage.
	Name string

	// Status is the current lifecycle state.
	Status StageStatus

	// Progress is a percentage (0-100) of stage completion.
	Progress float64

	// Error describes the reason for failure, empty when not failed.
	Error string

	// Dependencies lists stage names that must be completed before
	// this stage can start.
	Dependencies []string

	// Attempts is the number of times this stage has been started
	// (including retries).
	Attempts int
}

// SyncPipeline orchestrates multi-stage synchronization. Stages have
// explicit dependencies and progress through pending -> running ->
// completed/failed. All public methods are safe for concurrent use.
type SyncPipeline struct {
	config PipelineConfig

	mu     gosync.RWMutex
	stages map[string]*PipelineStage
	order  []string // insertion order for deterministic iteration
}

// NewSyncPipeline creates a new pipeline with the given config.
func NewSyncPipeline(config PipelineConfig) *SyncPipeline {
	return &SyncPipeline{
		config: config,
		stages: make(map[string]*PipelineStage),
	}
}

// AddStage registers a new stage with the given name and dependencies.
// Dependencies must refer to stages that are already registered or will
// be registered before the stage is started. Returns an error if a stage
// with the same name already exists.
func (p *SyncPipeline) AddStage(name string, dependencies []string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.stages[name]; exists {
		return fmt.Errorf("%w: stage %q already exists", ErrPipelineStageActive, name)
	}

	deps := make([]string, len(dependencies))
	copy(deps, dependencies)

	p.stages[name] = &PipelineStage{
		Name:         name,
		Status:       StageStatusPending,
		Dependencies: deps,
	}
	p.order = append(p.order, name)
	return nil
}

// StartStage transitions a stage from pending to running. It verifies
// that all dependencies are completed and that the concurrent stage
// limit is not exceeded.
func (p *SyncPipeline) StartStage(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	stage, ok := p.stages[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrPipelineStageNotFound, name)
	}

	if stage.Status != StageStatusPending {
		return fmt.Errorf("%w: stage %q is %s", ErrPipelineStageActive, name, stage.Status)
	}

	// Check dependencies.
	for _, dep := range stage.Dependencies {
		depStage, depOK := p.stages[dep]
		if !depOK {
			return fmt.Errorf("%w: dependency %q of %q not found", ErrPipelineStageDeps, dep, name)
		}
		if depStage.Status != StageStatusCompleted {
			return fmt.Errorf("%w: dependency %q of %q is %s (need completed)",
				ErrPipelineStageDeps, dep, name, depStage.Status)
		}
	}

	// Check concurrent stage limit.
	if p.config.MaxConcurrentStages > 0 {
		running := p.runningCountLocked()
		if running >= p.config.MaxConcurrentStages {
			return fmt.Errorf("%w: %d stages already running (max %d)",
				ErrPipelineStageActive, running, p.config.MaxConcurrentStages)
		}
	}

	stage.Status = StageStatusRunning
	stage.Attempts++
	return nil
}

// CompleteStage marks a running stage as completed.
func (p *SyncPipeline) CompleteStage(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	stage, ok := p.stages[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrPipelineStageNotFound, name)
	}

	if stage.Status != StageStatusRunning {
		return fmt.Errorf("%w: stage %q is %s, not running",
			ErrPipelineStageActive, name, stage.Status)
	}

	stage.Status = StageStatusCompleted
	stage.Progress = 100.0
	stage.Error = ""
	return nil
}

// FailStage marks a running stage as failed with the given reason.
func (p *SyncPipeline) FailStage(name string, reason string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	stage, ok := p.stages[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrPipelineStageNotFound, name)
	}

	if stage.Status != StageStatusRunning {
		return fmt.Errorf("%w: stage %q is %s, not running",
			ErrPipelineStageActive, name, stage.Status)
	}

	stage.Status = StageStatusFailed
	stage.Error = reason
	return nil
}

// RetryStage transitions a failed stage back to pending so it can be
// started again. Returns ErrPipelineRetryExhausted if the retry limit
// has been reached.
func (p *SyncPipeline) RetryStage(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	stage, ok := p.stages[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrPipelineStageNotFound, name)
	}

	if stage.Status != StageStatusFailed {
		return fmt.Errorf("%w: stage %q is %s, not failed",
			ErrPipelineStageActive, name, stage.Status)
	}

	if p.config.RetryLimit > 0 && stage.Attempts >= p.config.RetryLimit {
		return fmt.Errorf("%w: stage %q has %d attempts (limit %d)",
			ErrPipelineRetryExhausted, name, stage.Attempts, p.config.RetryLimit)
	}

	stage.Status = StageStatusPending
	stage.Error = ""
	return nil
}

// UpdateStageProgress sets the progress percentage for a running stage.
func (p *SyncPipeline) UpdateStageProgress(name string, progress float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	stage, ok := p.stages[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrPipelineStageNotFound, name)
	}

	if progress < 0 {
		progress = 0
	} else if progress > 100 {
		progress = 100
	}

	stage.Progress = progress
	return nil
}

// Progress returns a snapshot map of all pipeline stages, keyed by name.
// The returned PipelineStage values are copies.
func (p *SyncPipeline) Progress() map[string]*PipelineStage {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]*PipelineStage, len(p.stages))
	for name, stage := range p.stages {
		cp := *stage
		cp.Dependencies = make([]string, len(stage.Dependencies))
		copy(cp.Dependencies, stage.Dependencies)
		result[name] = &cp
	}
	return result
}

// OverallProgress returns the overall pipeline completion as a
// percentage (0-100). Each stage contributes equally.
func (p *SyncPipeline) OverallProgress() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.stages) == 0 {
		return 0
	}

	var total float64
	for _, stage := range p.stages {
		total += stage.Progress
	}
	return total / float64(len(p.stages))
}

// IsComplete returns true if all stages have completed.
func (p *SyncPipeline) IsComplete() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.stages) == 0 {
		return false
	}

	for _, stage := range p.stages {
		if stage.Status != StageStatusCompleted {
			return false
		}
	}
	return true
}

// HasFailed returns true if any stage is in a failed state.
func (p *SyncPipeline) HasFailed() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, stage := range p.stages {
		if stage.Status == StageStatusFailed {
			return true
		}
	}
	return false
}

// StageCount returns the total number of registered stages.
func (p *SyncPipeline) StageCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.stages)
}

// GetStage returns a copy of a single stage by name.
func (p *SyncPipeline) GetStage(name string) (*PipelineStage, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stage, ok := p.stages[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrPipelineStageNotFound, name)
	}

	cp := *stage
	cp.Dependencies = make([]string, len(stage.Dependencies))
	copy(cp.Dependencies, stage.Dependencies)
	return &cp, nil
}

// StageNames returns the stage names in insertion order.
func (p *SyncPipeline) StageNames() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	names := make([]string, len(p.order))
	copy(names, p.order)
	return names
}

// Reset clears all stages and returns the pipeline to its initial state.
func (p *SyncPipeline) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stages = make(map[string]*PipelineStage)
	p.order = nil
}

// runningCountLocked returns the number of currently running stages.
// Caller must hold at least a read lock.
func (p *SyncPipeline) runningCountLocked() int {
	count := 0
	for _, stage := range p.stages {
		if stage.Status == StageStatusRunning {
			count++
		}
	}
	return count
}
