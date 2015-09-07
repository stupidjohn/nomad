package scheduler

import (
	"fmt"
	"testing"

	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
)

func TestServiceSched_JobRegister(t *testing.T) {
	h := NewHarness(t)

	// Create some nodes
	for i := 0; i < 10; i++ {
		node := mock.Node()
		noErr(t, h.State.RegisterNode(h.NextIndex(), node))
	}

	// Create a job
	job := mock.Job()
	noErr(t, h.State.RegisterJob(h.NextIndex(), job))

	// Create a mock evaluation to deregister the job
	eval := &structs.Evaluation{
		ID:          mock.GenerateUUID(),
		Priority:    job.Priority,
		TriggeredBy: structs.EvalTriggerJobRegister,
		JobID:       job.ID,
	}

	// Process the evaluation
	err := h.Process(NewServiceScheduler, eval)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Ensure a single plan
	if len(h.Plans) != 1 {
		t.Fatalf("bad: %#v", h.Plans)
	}
	plan := h.Plans[0]

	// Ensure the plan allocated
	var planned []*structs.Allocation
	for _, allocList := range plan.NodeAllocation {
		planned = append(planned, allocList...)
	}
	if len(planned) != 10 {
		t.Fatalf("bad: %#v", plan)
	}

	// Lookup the allocations by JobID
	out, err := h.State.AllocsByJob(job.ID)
	noErr(t, err)

	// Ensure all allocations placed
	if len(out) != 10 {
		t.Fatalf("bad: %#v", out)
	}

	h.AssertEvalStatus(t, structs.EvalStatusComplete)
}

func TestServiceSched_JobRegister_AllocFail(t *testing.T) {
	h := NewHarness(t)

	// Create NO nodes
	// Create a job
	job := mock.Job()
	noErr(t, h.State.RegisterJob(h.NextIndex(), job))

	// Create a mock evaluation to deregister the job
	eval := &structs.Evaluation{
		ID:          mock.GenerateUUID(),
		Priority:    job.Priority,
		TriggeredBy: structs.EvalTriggerJobRegister,
		JobID:       job.ID,
	}

	// Process the evaluation
	err := h.Process(NewServiceScheduler, eval)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Ensure a single plan
	if len(h.Plans) != 1 {
		t.Fatalf("bad: %#v", h.Plans)
	}
	plan := h.Plans[0]

	// Ensure the plan failed to alloc
	if len(plan.FailedAllocs) != 1 {
		t.Fatalf("bad: %#v", plan)
	}

	// Lookup the allocations by JobID
	out, err := h.State.AllocsByJob(job.ID)
	noErr(t, err)

	// Ensure all allocations placed
	if len(out) != 1 {
		t.Fatalf("bad: %#v", out)
	}

	// Check the coalesced failures
	if out[0].Metrics.CoalescedFailures != 9 {
		t.Fatalf("bad: %#v", out[0].Metrics)
	}

	h.AssertEvalStatus(t, structs.EvalStatusComplete)
}

func TestServiceSched_JobModify(t *testing.T) {
	h := NewHarness(t)

	// Create some nodes
	var nodes []*structs.Node
	for i := 0; i < 10; i++ {
		node := mock.Node()
		nodes = append(nodes, node)
		noErr(t, h.State.RegisterNode(h.NextIndex(), node))
	}

	// Generate a fake job with allocations
	job := mock.Job()
	noErr(t, h.State.RegisterJob(h.NextIndex(), job))

	var allocs []*structs.Allocation
	for i := 0; i < 10; i++ {
		alloc := mock.Alloc()
		alloc.Job = job
		alloc.JobID = job.ID
		alloc.NodeID = nodes[i].ID
		allocs = append(allocs, alloc)
	}
	noErr(t, h.State.UpdateAllocations(h.NextIndex(), allocs))

	// Update the job
	job2 := mock.Job()
	job2.ID = job.ID
	noErr(t, h.State.RegisterJob(h.NextIndex(), job2))

	// Create a mock evaluation to deal with drain
	eval := &structs.Evaluation{
		ID:          mock.GenerateUUID(),
		Priority:    50,
		TriggeredBy: structs.EvalTriggerJobRegister,
		JobID:       job.ID,
	}

	// Process the evaluation
	err := h.Process(NewServiceScheduler, eval)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Ensure a single plan
	if len(h.Plans) != 1 {
		t.Fatalf("bad: %#v", h.Plans)
	}
	plan := h.Plans[0]

	// Ensure the plan evicted all allocs
	var update []*structs.Allocation
	for _, updateList := range plan.NodeUpdate {
		update = append(update, updateList...)
	}
	if len(update) != len(allocs) {
		t.Fatalf("bad: %#v", plan)
	}

	// Ensure the plan allocated
	var planned []*structs.Allocation
	for _, allocList := range plan.NodeAllocation {
		planned = append(planned, allocList...)
	}
	if len(planned) != 10 {
		t.Fatalf("bad: %#v", plan)
	}

	// Lookup the allocations by JobID
	out, err := h.State.AllocsByJob(job.ID)
	noErr(t, err)

	// Ensure all allocations placed
	out = structs.FilterTerminalAllocs(out)
	if len(out) != 10 {
		t.Fatalf("bad: %#v", out)
	}

	h.AssertEvalStatus(t, structs.EvalStatusComplete)
}

func TestServiceSched_JobDeregister(t *testing.T) {
	h := NewHarness(t)

	// Generate a fake job with allocations
	job := mock.Job()

	var allocs []*structs.Allocation
	for i := 0; i < 10; i++ {
		alloc := mock.Alloc()
		alloc.Job = job
		alloc.JobID = job.ID
		allocs = append(allocs, alloc)
	}
	noErr(t, h.State.UpdateAllocations(h.NextIndex(), allocs))

	// Create a mock evaluation to deregister the job
	eval := &structs.Evaluation{
		ID:          mock.GenerateUUID(),
		Priority:    50,
		TriggeredBy: structs.EvalTriggerJobDeregister,
		JobID:       job.ID,
	}

	// Process the evaluation
	err := h.Process(NewServiceScheduler, eval)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Ensure a single plan
	if len(h.Plans) != 1 {
		t.Fatalf("bad: %#v", h.Plans)
	}
	plan := h.Plans[0]

	// Ensure the plan evicted all nodes
	if len(plan.NodeUpdate["foo"]) != len(allocs) {
		t.Fatalf("bad: %#v", plan)
	}

	// Lookup the allocations by JobID
	out, err := h.State.AllocsByJob(job.ID)
	noErr(t, err)

	// Ensure no remaining allocations
	out = structs.FilterTerminalAllocs(out)
	if len(out) != 0 {
		t.Fatalf("bad: %#v", out)
	}

	h.AssertEvalStatus(t, structs.EvalStatusComplete)
}

func TestServiceSched_NodeDrain(t *testing.T) {
	h := NewHarness(t)

	// Register a draining node
	node := mock.Node()
	node.Drain = true
	noErr(t, h.State.RegisterNode(h.NextIndex(), node))

	// Create some nodes
	for i := 0; i < 10; i++ {
		node := mock.Node()
		noErr(t, h.State.RegisterNode(h.NextIndex(), node))
	}

	// Generate a fake job with allocations
	job := mock.Job()
	noErr(t, h.State.RegisterJob(h.NextIndex(), job))

	var allocs []*structs.Allocation
	for i := 0; i < 10; i++ {
		alloc := mock.Alloc()
		alloc.Job = job
		alloc.JobID = job.ID
		alloc.NodeID = node.ID
		alloc.Name = fmt.Sprintf("my-job.web[%d]", i)
		allocs = append(allocs, alloc)
	}
	noErr(t, h.State.UpdateAllocations(h.NextIndex(), allocs))

	// Create a mock evaluation to deal with drain
	eval := &structs.Evaluation{
		ID:          mock.GenerateUUID(),
		Priority:    50,
		TriggeredBy: structs.EvalTriggerNodeUpdate,
		JobID:       job.ID,
		NodeID:      node.ID,
	}

	// Process the evaluation
	err := h.Process(NewServiceScheduler, eval)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Ensure a single plan
	if len(h.Plans) != 1 {
		t.Fatalf("bad: %#v", h.Plans)
	}
	plan := h.Plans[0]

	// Ensure the plan evicted all allocs
	if len(plan.NodeUpdate[node.ID]) != len(allocs) {
		t.Fatalf("bad: %#v", plan)
	}

	// Ensure the plan allocated
	var planned []*structs.Allocation
	for _, allocList := range plan.NodeAllocation {
		planned = append(planned, allocList...)
	}
	if len(planned) != 10 {
		t.Fatalf("bad: %#v", plan)
	}

	// Lookup the allocations by JobID
	out, err := h.State.AllocsByJob(job.ID)
	noErr(t, err)

	// Ensure all allocations placed
	out = structs.FilterTerminalAllocs(out)
	if len(out) != 10 {
		t.Fatalf("bad: %#v", out)
	}

	h.AssertEvalStatus(t, structs.EvalStatusComplete)
}

func TestServiceSched_RetryLimit(t *testing.T) {
	h := NewHarness(t)
	h.Planner = &RejectPlan{h}

	// Create some nodes
	for i := 0; i < 10; i++ {
		node := mock.Node()
		noErr(t, h.State.RegisterNode(h.NextIndex(), node))
	}

	// Create a job
	job := mock.Job()
	noErr(t, h.State.RegisterJob(h.NextIndex(), job))

	// Create a mock evaluation to deregister the job
	eval := &structs.Evaluation{
		ID:          mock.GenerateUUID(),
		Priority:    job.Priority,
		TriggeredBy: structs.EvalTriggerJobRegister,
		JobID:       job.ID,
	}

	// Process the evaluation
	err := h.Process(NewServiceScheduler, eval)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	// Ensure multiple plans
	if len(h.Plans) == 0 {
		t.Fatalf("bad: %#v", h.Plans)
	}

	// Lookup the allocations by JobID
	out, err := h.State.AllocsByJob(job.ID)
	noErr(t, err)

	// Ensure no allocations placed
	if len(out) != 0 {
		t.Fatalf("bad: %#v", out)
	}

	// Should hit the retry limit
	h.AssertEvalStatus(t, structs.EvalStatusFailed)
}
