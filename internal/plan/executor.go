package plan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Executor executes build plans using worker pools
type Executor struct {
	workers     int
	taskTimeout time.Duration
	taskChan    chan Task
	resultChan  chan ExecutionResult
	poolCtx     context.Context
	poolCancel  context.CancelFunc
	wg          sync.WaitGroup
	queueWg     sync.WaitGroup // Track queue goroutines
	shutdown    bool
	shutdownMu  sync.RWMutex
}

// Task represents a unit of work for the worker pool
type Task struct {
	Command *BuildCommand
	Done    chan error
}

// ExecutionResult represents the result of executing a build command
type ExecutionResult struct {
	Command   *BuildCommand
	StartTime time.Time
	EndTime   time.Time
	Output    string
	Error     error
}

// NewExecutor creates a new plan executor
func NewExecutor(workers int, taskTimeout time.Duration) *Executor {
	if workers <= 0 {
		workers = 1
	}
	if taskTimeout == 0 {
		taskTimeout = 300 * time.Second // Default 5 minutes
	}

	poolCtx, poolCancel := context.WithCancel(context.Background())

	e := &Executor{
		workers:     workers,
		taskTimeout: taskTimeout,
		taskChan:    make(chan Task),
		resultChan:  make(chan ExecutionResult, workers*2),
		poolCtx:     poolCtx,
		poolCancel:  poolCancel,
	}

	// Start worker goroutines
	for i := 0; i < workers; i++ {
		e.wg.Add(1)
		go e.worker(poolCtx)
	}

	return e
}

// worker processes tasks from the task channel
func (e *Executor) worker(poolCtx context.Context) {
	defer e.wg.Done()

	for {
		select {
		case <-poolCtx.Done():
			// Pool cancelled, worker exits
			return

		case task, ok := <-e.taskChan:
			if !ok {
				// Channel closed
				return
			}

			// Create timeout context for this task
			taskCtx, cancel := context.WithTimeout(poolCtx, e.taskTimeout)
			e.executeTask(taskCtx, task)
			cancel() // Clean up task context
		}
	}
}

// runSteps runs an ordered sequence of commands, stopping on the first failure,
// and returns the combined output. Each command inherits the process environment
// (so registry credentials are visible).
func (e *Executor) runSteps(ctx context.Context, steps [][]string) (string, error) {
	var out strings.Builder
	for _, step := range steps {
		if len(step) == 0 {
			continue
		}
		cmd := exec.CommandContext(ctx, step[0], step[1:]...)
		cmd.Env = os.Environ()
		o, err := cmd.CombinedOutput()
		out.Write(o)
		if err != nil {
			return out.String(), err
		}
	}
	return out.String(), nil
}

// executeTask executes a single task (its full step sequence) with timeout
func (e *Executor) executeTask(ctx context.Context, task Task) {
	result := ExecutionResult{
		Command:   task.Command,
		StartTime: time.Now(),
	}

	// A pushing task carries an ordered sequence (build, push per arch, manifest);
	// otherwise it is the single build command.
	steps := task.Command.Steps
	if len(steps) == 0 {
		steps = [][]string{task.Command.Command}
	}

	output, err := e.runSteps(ctx, steps)
	result.Output = output
	result.Error = err
	result.EndTime = time.Now()

	// Send result to channel
	select {
	case e.resultChan <- result:
		// Result sent successfully
	case <-ctx.Done():
		// Context cancelled, don't block
	}

	// Signal completion
	if task.Done != nil {
		select {
		case task.Done <- err:
		case <-ctx.Done():
			// Context cancelled while sending
		}
	}
}

// Execute runs the build plan
func (e *Executor) Execute(ctx context.Context, plan *BuildPlan) ([]ExecutionResult, error) {
	// Check if already shut down
	e.shutdownMu.RLock()
	isShutdown := e.shutdown
	e.shutdownMu.RUnlock()
	if isShutdown {
		return nil, fmt.Errorf("executor is already shut down, create a new executor instance")
	}

	defer e.Shutdown() // Ensure cleanup

	// Validate plan first
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("invalid plan: %w", err)
	}

	// Registry login once, before any queue. Abort the run if it fails.
	if len(plan.Login) > 0 {
		if _, err := e.runSteps(ctx, plan.Login); err != nil {
			return nil, fmt.Errorf("registry login failed: %w", err)
		}
	}

	// Process queues
	return e.executeQueues(ctx, plan)
}

// executeQueues processes multiple queues in parallel
func (e *Executor) executeQueues(ctx context.Context, plan *BuildPlan) ([]ExecutionResult, error) {
	// Start collector goroutine for results
	results := []ExecutionResult{}
	resultsDone := make(chan struct{})
	go func() {
		for result := range e.resultChan {
			results = append(results, result)
		}
		close(resultsDone)
	}()

	// Collect per-queue failures so Execute can report a non-zero error.
	var queueErrMu sync.Mutex
	var queueErrs []error

	// Process each queue in parallel
	for _, queue := range plan.Queues {
		e.queueWg.Add(1)
		go func(q Queue) {
			defer e.queueWg.Done()

			log.Info().Str("queue", q.ID).Msg("starting queue")

			// Process batches sequentially within this queue
			for batchIdx, batch := range q.Batches {
				log.Info().Str("queue", q.ID).Int("batch", batchIdx+1).Int("tasks", len(batch.Tasks)).Msg("executing batch")

				if err := e.processBatch(ctx, batch, q.Commands); err != nil {
					// Record the failure and stop this queue (other queues continue).
					log.Error().Err(err).Str("queue", q.ID).Int("batch", batchIdx+1).Msg("batch failed")
					queueErrMu.Lock()
					queueErrs = append(queueErrs, fmt.Errorf("queue %s batch %d: %w", q.ID, batchIdx+1, err))
					queueErrMu.Unlock()
					return
				}
			}

			log.Info().Str("queue", q.ID).Msg("completed queue")
		}(queue)
	}

	// Monitor for context cancellation while waiting
	done := make(chan struct{})
	go func() {
		e.queueWg.Wait()
		close(done)
	}()

	var executionErr error

	select {
	case <-done:
		// All queues finished normally
		// Just close the result channel
		close(e.resultChan)
	case <-ctx.Done():
		// Context cancelled - stop processing
		log.Warn().Msg("context cancelled, stopping execution")
		executionErr = ctx.Err()
		// Cancel workers and wait for them
		e.poolCancel()
		e.wg.Wait()
		// Now safe to close result channel
		close(e.resultChan)
	}

	// Wait for collector to finish gathering results
	<-resultsDone

	// Surface queue failures (context errors take precedence).
	if executionErr == nil && len(queueErrs) > 0 {
		executionErr = errors.Join(queueErrs...)
	}

	return results, executionErr
}

// processBatch processes all tasks in a batch
func (e *Executor) processBatch(ctx context.Context, batch Batch, commands []BuildCommand) error {
	done := make(chan error, len(batch.Tasks))

	// Send all tasks to workers
	for _, taskID := range batch.Tasks {
		// Find the command for this task
		var cmd *BuildCommand
		for i := range commands {
			if commands[i].ImageTag == taskID {
				cmd = &commands[i]
				break
			}
		}

		if cmd == nil {
			return fmt.Errorf("command not found for task %s", taskID)
		}

		task := Task{
			Command: cmd,
			Done:    done,
		}

		// Check if executor is shutting down
		e.shutdownMu.RLock()
		if e.shutdown {
			e.shutdownMu.RUnlock()
			return fmt.Errorf("executor is shutting down")
		}
		e.shutdownMu.RUnlock()

		select {
		case e.taskChan <- task:
			// Task sent to worker
		case <-ctx.Done():
			return ctx.Err()
		case <-e.poolCtx.Done():
			return fmt.Errorf("executor pool cancelled")
		}
	}

	// Wait for all tasks in batch to complete
	var firstErr error
	for i := 0; i < len(batch.Tasks); i++ {
		select {
		case err := <-done:
			if err != nil && firstErr == nil {
				firstErr = err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return firstErr
}

// Shutdown cleanly shuts down the executor
func (e *Executor) Shutdown() {
	// Mark as shutting down
	e.shutdownMu.Lock()
	if e.shutdown {
		e.shutdownMu.Unlock()
		return // Already shut down
	}
	e.shutdown = true
	e.shutdownMu.Unlock()

	e.poolCancel()    // Cancel pool context (stops all workers)
	e.queueWg.Wait()  // Wait for all queue goroutines to finish
	close(e.taskChan) // Now safe to close - no more senders
	e.wg.Wait()       // Wait for workers to exit
}
