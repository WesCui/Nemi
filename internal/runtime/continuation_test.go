package runtime

import (
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"
	"nemi/internal/store"
	"testing"
	"time"
)

func TestContinuationWaitsForDurableSignalWithoutModelWork(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivity(&Activities{})
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	env.SetStartTime(now)
	ref := store.Ref{Workspace: "w", ID: "r"}
	env.OnActivity("AdvanceContinuation", mock.Anything, ref).Return(store.ContinuationCheck{Until: now.Add(24 * time.Hour)}, nil).Once()
	env.OnActivity("AdvanceContinuation", mock.Anything, ref).Return(store.ContinuationCheck{Done: true}, nil).Once()
	env.RegisterDelayedCallback(func() { env.SignalWorkflow(ContinuationSignal, 1) }, time.Hour)
	env.ExecuteWorkflow(ContinuationWorkflow, ref)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if !env.Now().Equal(now.Add(time.Hour)) {
		t.Fatal("signal did not release wait")
	}
	env.AssertExpectations(t)
}
func TestContinuationExpiryUsesDurableTimer(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivity(&Activities{})
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	env.SetStartTime(now)
	ref := store.Ref{Workspace: "w", ID: "r"}
	env.OnActivity("AdvanceContinuation", mock.Anything, ref).Return(store.ContinuationCheck{Until: now.Add(24 * time.Hour)}, nil).Once()
	env.OnActivity("AdvanceContinuation", mock.Anything, ref).Return(store.ContinuationCheck{Done: true}, nil).Once()
	env.ExecuteWorkflow(ContinuationWorkflow, ref)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatal(err)
	}
	if env.Now().Before(now.Add(24 * time.Hour)) {
		t.Fatal("wait expired early")
	}
	env.AssertExpectations(t)
}
