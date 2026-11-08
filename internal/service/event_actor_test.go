package service

import (
	"encoding/json"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func TestResolveRecipientsKeepsEveryoneWhenTheActorIsNotAUser(t *testing.T) {
	plan := &notificationPlan{
		Actor:  domain.FormActor(1, "Intake"),
		Direct: assignedTargets([]uint{1, 2}),
	}
	got := resolveRecipients(plan, nil)
	if len(got) != 2 || got[1] != domain.NotificationAssigned || got[2] != domain.NotificationAssigned {
		t.Fatalf("recipients = %v, a form with id 1 must not suppress user 1", got)
	}
	plan.Actor = domain.UserActor(1)
	if got := resolveRecipients(plan, nil); len(got) != 1 || got[2] == "" {
		t.Fatalf("a user actor must still be excluded: %v", got)
	}
}

func TestTaskCreatedByFormNotifiesAssigneesAndWatchesTheOwner(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 30)
	rig.run(domain.ActivityEntry{
		TaskID: 5, Actor: domain.FormActor(1, "Intake"), Action: domain.ActivityTaskCreated,
		NewValue: raw(map[string]any{"title": "From form", "assignee_ids": []uint{1}, "owner_id": 2}),
	})
	got := draftTypes(rig.inbox.drafts)
	if len(got) != 1 || got[1] != domain.NotificationAssigned {
		t.Fatalf("drafts = %v, want user 1 assigned", got)
	}
	watched := map[uint]bool{}
	for _, id := range rig.watchers.auto {
		watched[id] = true
	}
	if !watched[1] || !watched[2] || watched[0] || len(watched) != 2 {
		t.Fatalf("auto-watch = %v, want the assignee and the owner only", rig.watchers.auto)
	}
}

func TestNotificationForNonUserActorCarriesTheLabel(t *testing.T) {
	rig := newNotifyRig(t, allowPerms{}, 30)
	rig.run(domain.ActivityEntry{
		TaskID: 5, Actor: domain.FormActor(9, "Bug intake"), Action: domain.ActivityTaskCreated,
		NewValue: raw(map[string]any{"title": "x", "assignee_ids": []uint{2}}),
	})
	if len(rig.inbox.drafts) != 1 {
		t.Fatalf("drafts = %v", rig.inbox.drafts)
	}
	draft := rig.inbox.drafts[0]
	var payload map[string]any
	if err := json.Unmarshal(draft.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if draft.ActorID != 0 || payload["actor_type"] != "form" || payload["actor_name"] != "Bug intake" {
		t.Fatalf("draft = %+v payload = %v", draft, payload)
	}
}

func submissionAnswer(questionID uint, value string) domain.SubmissionAnswer {
	return domain.SubmissionAnswer{QuestionID: questionID, Value: json.RawMessage(`"` + value + `"`)}
}

func assigneeConfig() domain.FormTaskConfiguration {
	return domain.FormTaskConfiguration{
		TaskDefaults: domain.FormTaskDefaults{Priority: "normal", StatusID: new(uint)},
		Mappings: []domain.FormTaskMapping{
			{QuestionID: 1, Target: domain.FormMappingTarget{Type: "task_field", Field: "title"}},
			{QuestionID: 2, Target: domain.FormMappingTarget{Type: "task_field", Field: "assignee"}},
		},
	}
}

func TestAssigneeMappingResolvesMembersAndRecordsFailures(t *testing.T) {
	config := assigneeConfig()
	*config.TaskDefaults.StatusID = 3
	cases := []struct {
		answer      string
		members     map[uint]bool
		wantAssigns []uint
		wantFailure string
	}{
		{"7", map[uint]bool{7: true}, []uint{7}, ""},
		{"7", map[uint]bool{8: true}, nil, "NOT_A_MEMBER"},
		{"abc", map[uint]bool{7: true}, nil, "INVALID_ID"},
		{"0", map[uint]bool{7: true}, nil, "INVALID_ID"},
	}
	for _, tc := range cases {
		submission := domain.FormSubmission{ID: 1, Answers: []domain.SubmissionAnswer{submissionAnswer(1, "T"), submissionAnswer(2, tc.answer)}}
		resolved, err := resolveSubmissionTask(submission, config, TaskTemplateConfiguration{}, tc.members)
		if err != nil {
			t.Fatalf("answer %q: %v", tc.answer, err)
		}
		if len(resolved.input.AssigneeIDs) != len(tc.wantAssigns) {
			t.Fatalf("answer %q assignees = %v, want %v", tc.answer, resolved.input.AssigneeIDs, tc.wantAssigns)
		}
		for i, id := range tc.wantAssigns {
			if resolved.input.AssigneeIDs[i] != id {
				t.Fatalf("answer %q assignees = %v, want %v", tc.answer, resolved.input.AssigneeIDs, tc.wantAssigns)
			}
		}
		var snapshot []map[string]any
		if err := json.Unmarshal(resolved.snapshot, &snapshot); err != nil || len(snapshot) != 2 {
			t.Fatalf("snapshot = %s (%v)", resolved.snapshot, err)
		}
		failure, _ := snapshot[1]["failure"].(string)
		if failure != tc.wantFailure {
			t.Fatalf("answer %q failure = %q, want %q", tc.answer, failure, tc.wantFailure)
		}
		if _, ok := snapshot[0]["failure"]; ok {
			t.Fatalf("the title mapping must not carry a failure: %s", resolved.snapshot)
		}
	}
}

func TestRequesterMappingForAnonymousAndAuthenticatedRespondents(t *testing.T) {
	config := assigneeConfig()
	*config.TaskDefaults.StatusID = 3
	config.Mappings = []domain.FormTaskMapping{
		{QuestionID: 1, Target: domain.FormMappingTarget{Type: "task_field", Field: "title"}},
		{QuestionID: 2, Target: domain.FormMappingTarget{Type: "requester", Field: "name"}},
		{QuestionID: 3, Target: domain.FormMappingTarget{Type: "requester", Field: "email"}},
	}
	submission := domain.FormSubmission{ID: 1, Answers: []domain.SubmissionAnswer{
		submissionAnswer(1, "T"), submissionAnswer(2, "  Pat  "), submissionAnswer(3, " pat@example.test "),
	}}
	resolved, err := resolveSubmissionTask(submission, config, TaskTemplateConfiguration{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.requester == nil || resolved.requester.Name != "Pat" || resolved.requester.Email != "pat@example.test" || resolved.requester.UserID != nil {
		t.Fatalf("requester = %+v", resolved.requester)
	}

	respondent := uint(11)
	submission.RespondentID = &respondent
	resolved, err = resolveSubmissionTask(submission, config, TaskTemplateConfiguration{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.requester == nil || resolved.requester.UserID == nil || *resolved.requester.UserID != 11 ||
		resolved.requester.Name != "" || resolved.requester.Email != "" {
		t.Fatalf("an authenticated respondent must win over answers: %+v", resolved.requester)
	}

	submission.RespondentID = nil
	submission.Answers = submission.Answers[:1]
	resolved, err = resolveSubmissionTask(submission, config, TaskTemplateConfiguration{}, nil)
	if err != nil || resolved.requester != nil {
		t.Fatalf("no answers and no respondent must leave the requester empty: %+v, %v", resolved.requester, err)
	}
}

func TestOwnerCandidatesAndMembership(t *testing.T) {
	config := assigneeConfig()
	owner := uint(4)
	config.OwnerID = &owner
	submission := domain.FormSubmission{Answers: []domain.SubmissionAnswer{submissionAnswer(2, "9")}}
	ids := submissionUserCandidates(submission, config)
	if len(ids) != 2 || ids[0] != 4 || ids[1] != 9 {
		t.Fatalf("candidates = %v", ids)
	}
	*config.TaskDefaults.StatusID = 3
	submission.Answers = append(submission.Answers, submissionAnswer(1, "T"))
	resolved, err := resolveSubmissionTask(submission, config, TaskTemplateConfiguration{}, memberSet([]uint{4}))
	if err != nil || !resolved.ownerIsMember {
		t.Fatalf("owner is a member: %+v, %v", resolved, err)
	}
	resolved, err = resolveSubmissionTask(submission, config, TaskTemplateConfiguration{}, memberSet([]uint{9}))
	if err != nil || resolved.ownerIsMember {
		t.Fatalf("owner left the workspace: %+v, %v", resolved, err)
	}
}
