//go:build ee

package service

import (
	"fmt"
	"time"

	"ragflow/internal/common"
	servicepkg "ragflow/internal/service"
)

func (e *Ingestor) handleEvaluationTask(handle common.TaskHandle, hb *Heartbeat) bool {
	msg := handle.GetMessage()
	if msg.TaskType != "evaluation" {
		return false
	}
	if e.deps.DB == nil || msg.TaskID == "" {
		e.ackHandle(hb, handle, msg.TaskID)
		return true
	}
	runner := servicepkg.NewEvaluationRunner(e.deps.DB, servicepkg.NewMessageQueueTaskPublisherWithEngine(e.deps.MQ))
	ack := false
	defer func() {
		hb.Stop()
		if recovered := recover(); recovered != nil {
			common.Error("evaluation task panicked", fmt.Errorf("%v", recovered))
		}
		if ack && !e.leaseAbandoned(hb) {
			if err := handle.Ack(); err != nil {
				common.Error("ack evaluation task", err)
			}
		}
	}()
	var err error
	ack, err = runner.Handle(e.ctx, msg.TaskID, common.GenerateUUID())
	if err != nil && e.ctx.Err() == nil {
		common.Error("evaluation task failed", err)
	}
	return true
}

func (e *Ingestor) startEvaluationTaskReconciler() {
	if e.deps.DB == nil || e.deps.MQ == nil {
		return
	}
	e.workerWg.Go(func() {
		runner := servicepkg.NewEvaluationRunner(e.deps.DB, servicepkg.NewMessageQueueTaskPublisherWithEngine(e.deps.MQ))
		reconcile := func() {
			if err := runner.Reconcile(e.ctx); err != nil && e.ctx.Err() == nil {
				common.Error("reconcile evaluations", err)
			}
		}
		reconcile()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-e.ctx.Done():
				return
			case <-ticker.C:
				reconcile()
			}
		}
	})
}
