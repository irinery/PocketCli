package query

import (
	"context"

	"pocketcli/internal/investigation/contract"
)

type Service interface {
	Available() bool
	Execute(context.Context, string, contract.QueryRequest) (contract.QueryResult, error)
	Publish(string, Dataset) error
	VerifyPublished(string, string) error
}

type Dataset struct {
	Snapshot     contract.Snapshot
	Components   []contract.ComponentRecord
	Findings     []contract.Finding
	Evidence     []contract.Evidence
	Coverage     []contract.Coverage
	Issues       []Issue
	PullRequests []PullRequest
	Links        []contract.LinkRecord
	Events       []Event
	Stages       []contract.StageRecord
	Estimates    []contract.EstimateRecord
	Rework       []contract.ReworkRecord
	Deliveries   []contract.DeliveryRecord
	Metrics      []contract.Metric
}

type Event struct{ ID, EntityID, Type, EventAt, ObservedAt, ActorID, OldValue, NewValue, EvidenceID string }

type Issue struct {
	ID, Identifier, Source, Title, State, AssigneeID, TeamID, ComponentID string
	CreatedAt, UpdatedAt, StartedAt, CompletedAt                          string
	EstimateBeforeStart, EstimateCurrent                                  *float64
	ReopenCount                                                           int
	IsContext, DurationValid                                              bool
	CycleSeconds                                                          *int64
	EvidenceID                                                            string
}
type PullRequest struct {
	ID, Repository, Title, AuthorID, State, CreatedAt, UpdatedAt, ReadyAt, FirstHumanReviewAt, MergedAt, HeadSHA, MergeSHA, EvidenceID string
	Number                                                                                                                             int
	ReviewWaitSeconds                                                                                                                  *int64
}
