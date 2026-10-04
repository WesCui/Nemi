package runtime

import (
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"nemi/internal/store"
	"testing"
	"time"
)

func TestReminderUsesDurableTimer(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	env.SetStartTime(start)
	a := &Activities{}
	env.RegisterActivity(a)
	ref := store.Ref{Workspace: "w", ID: "r"}
	env.OnActivity("Deliver", mock.Anything, ref, 3).Return(nil).Once()
	env.ExecuteWorkflow(ReminderWorkflow, ReminderInput{Ref: ref, Revision: 3, Due: start.Add(90 * 24 * time.Hour)})
	if e := env.GetWorkflowError(); e != nil {
		t.Fatal(e)
	}
	if env.Now().Before(start.Add(90 * 24 * time.Hour)) {
		t.Fatal("reminder fired before its durable timer")
	}
	env.AssertExpectations(t)
}
func TestRunWaitsForAdmissionWithoutCallingModel(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivity(&Activities{})
	r := store.Ref{Workspace: "w", ID: "run"}
	env.OnActivity("Admit", mock.Anything, r).Return(Admission{}, nil).Once()
	env.OnActivity("Admit", mock.Anything, r).Return(Admission{Ready: true}, nil).Once()
	env.OnActivity("Generate", mock.Anything, r).Return(nil).Once()
	env.ExecuteWorkflow(RunWorkflow, r)
	if e := env.GetWorkflowError(); e != nil {
		t.Fatal(e)
	}
	env.AssertExpectations(t)
}
func TestPaidSubmissionNotRetriedAfterUnknownOutcome(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivity(&Activities{})
	r := store.Ref{Workspace: "w", ID: "run"}
	env.OnActivity("Admit", mock.Anything, r).Return(Admission{Ready: true}, nil).Once()
	env.OnActivity("Generate", mock.Anything, r).Return(temporal.NewApplicationError("interrupted", "UNKNOWN")).Once()
	env.OnActivity("Fail", mock.Anything, r, "MODEL_INTERRUPTED_OR_FAILED").Return(nil).Once()
	env.ExecuteWorkflow(RunWorkflow, r)
	if e := env.GetWorkflowError(); e != nil {
		t.Fatal(e)
	}
	env.AssertExpectations(t)
}
